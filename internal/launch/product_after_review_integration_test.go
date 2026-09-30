//go:build integration

package launch

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"retrom/internal/composition/cleanupjobs"
	dbapi "retrom/internal/database"
	contentrepo "retrom/internal/persistence/gamecontent"
	saverepo "retrom/internal/persistence/saves"
	"retrom/internal/service/gamecontent"
	"retrom/internal/service/saves"
	"retrom/internal/testsupport/workflowfixture"
)

func TestPublishedProductWorksAfterEntireReviewAndImportProcessIsDeleted(t *testing.T) {
	t.Parallel()
	fixture := newProductRPGFixture(t, "rpgxp")
	workflowfixture.DeleteFinishedProcess(t, fixture.database, fixture.blobs, fixture.itemID)
	_, saveID := productRPGSavedLaunch(t, fixture, "rpgmaker-xp")
	resumed, err := fixture.service.Create(t.Context(), "local", CreateRequest{
		GameID: fixture.gameID, SaveStateID: &saveID, ReturnTo: "/games/" + fixture.gameID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Config(t.Context(), resumed.LaunchID, resumed.Capability); err != nil {
		t.Fatal(err)
	}
	saver := saves.New(saverepo.New(fixture.database), fixture.blobs, fixture.now)
	restore, err := saver.StateFile(t.Context(), resumed.LaunchID, resumed.Capability)
	if err != nil || restore.Size == 0 {
		t.Fatalf("restore after process deletion: %+v %v", restore, err)
	}
	assertGameDeletionAfterProcessDeletion(t, fixture)
}

func assertGameDeletionAfterProcessDeletion(t *testing.T, fixture productRPGFixture) {
	t.Helper()
	repository := contentrepo.New(fixture.database)
	service := gamecontent.New(gamecontent.Dependencies{Repository: repository, Files: fixture.blobs}, gamecontent.Options{Now: fixture.now})
	detail, err := service.AdminGame(t.Context(), fixture.gameID)
	if err != nil {
		t.Fatal(err)
	}
	impact, err := gamecontent.NewImpactQueries(contentrepo.NewImpactQueries(fixture.database)).Game(t.Context(), fixture.gameID)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := service.DeleteAdminGame(t.Context(), gamecontent.DeleteGameRequest{
		GameID: fixture.gameID, PrincipalID: "rpg-product-admin", Key: "delete-without-workflow",
		RequestDigest: strings.Repeat("a", 64), ExpectedVersion: detail.Version, ConfirmTitle: detail.Title,
		ImpactDigest: impact.ImpactDigest, Actor: gamecontent.AuditActor{Kind: "USER", UserID: "rpg-product-admin"},
	})
	if err != nil || !deleted.PayloadReleaseQueued {
		t.Fatalf("delete after process deletion: %+v %v", deleted, err)
	}
	assertDeletedProductPayloadReleased(t, fixture)
}

func assertDeletedProductPayloadReleased(t *testing.T, fixture productRPGFixture) {
	t.Helper()
	var contentRecord string
	if err := dbapi.QueryRowContext(t.Context(), fixture.database, `SELECT file_record FROM game_files WHERE game_id=? LIMIT 1`, fixture.gameID).Scan(&contentRecord); err != nil {
		t.Fatal(err)
	}
	now := fixture.now()
	collector, err := cleanupjobs.New(t.Context(), fixture.database, fixture.blobs, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(collector.Close)
	for attempt := 0; attempt < 50; attempt++ {
		now = now.Add(time.Second)
		if _, err := collector.RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
		var state string
		if err := dbapi.QueryRowContext(t.Context(), fixture.database, `SELECT payload_state FROM games WHERE id=?`, fixture.gameID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "RELEASED" {
			file, err := fixture.blobs.OpenRecord(contentRecord)
			if errors.Is(err, os.ErrNotExist) {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Fatal("game deletion still needs removed review/import data")
}
