//go:build integration

package launch

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	persistence "retrom/internal/repo/launch"
	application "retrom/internal/service/launch"
)

func TestRPGProductRepeatedNonDefaultTargetKeepsPublishedVariant(t *testing.T) {
	fixture := newProductRPGFixture(t, "rpgxp")
	original, saveID := productRPGSavedLaunch(t, fixture, "rpgmaker-xp")
	for _, mode := range []string{"explicit", "default", "saved"} {
		t.Run(mode, func(t *testing.T) {
			previous := original.LaunchID
			for attempt := range 2 {
				command := productRPGCommand(fixture.gameID, mode, saveID, attempt)
				result, err := fixture.service.CreateProduct(t.Context(), command)
				if err != nil || result.Status != 201 || result.Created.LaunchID == "" || result.Created.LaunchID == previous {
					t.Fatalf("repeat %s/%d status=%d launch=%q error=%v", mode, attempt, result.Status, result.Created.LaunchID, err)
				}
				assertProductRPGTarget(t, fixture, result.Created, "rpgmaker-xp", mode == "saved", saveID)
				previous = result.Created.LaunchID
			}
		})
	}
	var variants, jobs int
	err := fixture.database.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM game_variants WHERE game_id=? AND core_id='rpgmaker'),(SELECT count(*) FROM jobs WHERE kind='VARIANT_VALIDATE')`, fixture.gameID).Scan(&variants, &jobs)
	if err != nil || variants != 1 || jobs != 0 {
		t.Fatalf("variants=%d jobs=%d error=%v", variants, jobs, err)
	}
}

func productRPGCommand(gameID, mode, saveID string, attempt int) application.ProductCreateCommand {
	request := CreateRequest{GameID: gameID, ReturnTo: "/games/" + gameID, ClientCapabilities: Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true}}
	if mode == "explicit" {
		core := "rpgmaker"
		request.CoreID = &core
	}
	if mode == "saved" {
		request.SaveStateID = &saveID
	}
	return application.ProductCreateCommand{
		ActorID: "rpg-product-admin", ProfileID: "local", Key: fmt.Sprintf("rpg-%s-%d", mode, attempt),
		Digest: strings.Repeat("a", 64), Request: request,
	}
}

func assertProductRPGTarget(t *testing.T, fixture productRPGFixture, created Created, target string, restore bool, saveID string) {
	t.Helper()
	var core, provider, selected, restored string
	err := fixture.database.QueryRowContext(t.Context(), `SELECT core_id,provider_id,target_id,COALESCE(save_state_id,'') FROM launch_sessions WHERE id=?`, created.LaunchID).Scan(&core, &provider, &selected, &restored)
	if err != nil || core != "rpgmaker" || provider != "retrom-runtime" || selected != target || restore && restored != saveID || !restore && restored != "" {
		t.Fatalf("launch identity=%s/%s/%s restore=%s error=%v", core, provider, selected, restored, err)
	}
	if _, err := fixture.service.Config(t.Context(), created.LaunchID, created.Capability); err != nil {
		t.Fatalf("resolved generation config: %v", err)
	}
}

func TestRPGProductSnapshotUsesExistingVariantTarget(t *testing.T) {
	fixture := newProductRPGFixture(t, "rpgxp")
	command := productRPGCommand(fixture.gameID, "explicit", "", 0)
	snapshot, err := persistence.NewProductCreation(fixture.database).Snapshot(t.Context(), command)
	if err != nil || !snapshot.Found || snapshot.Source.VariantID == "" || snapshot.Source.CoreID != "rpgmaker" || snapshot.Source.TargetID != "rpgmaker-xp" {
		t.Fatalf("source=%+v found=%v error=%v", snapshot.Source, snapshot.Found, err)
	}
}

func TestRPGProductRejectsUnavailableExistingTargetWithoutFallback(t *testing.T) {
	fixture := newProductRPGFixture(t, "rpgxp")
	mustRPGLaunchSQL(t, fixture.database, `UPDATE runtime_target_bindings SET launch_policy='DISABLED' WHERE core_id='rpgmaker' AND target_id='rpgmaker-xp'`)
	result, err := fixture.service.CreateProduct(t.Context(), productRPGCommand(fixture.gameID, "explicit", "", 0))
	if !errors.Is(err, ErrBlocked) || result.Created.LaunchID != "" {
		t.Fatalf("disabled published target status=%d launch=%q error=%v", result.Status, result.Created.LaunchID, err)
	}
}
