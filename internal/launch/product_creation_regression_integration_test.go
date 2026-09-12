//go:build integration

package launch

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"modernc.org/sqlite"
)

func productCreationFixture(t *testing.T) (reviewCheckpointFixture, CreateRequest) {
	t.Helper()
	fixture, created := newProductPlayFixture(t, false)
	var gameID string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT game_id FROM launch_sessions WHERE id=?`, created.LaunchID).Scan(&gameID); err != nil {
		t.Fatal(err)
	}
	return fixture, CreateRequest{GameID: gameID, ReturnTo: "/games/" + gameID}
}

func TestProductValidationIdentityRetainsEntropyFailure(t *testing.T) {
	fixture := newScummVMFixture(t, []string{"One"})
	published, err := fixture.importer.Approve(t.Context(), fixture.itemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	request := CreateRequest{GameID: published.GameID, ReturnTo: "/games/" + published.GameID, ClientCapabilities: Capabilities{SecureContext: true}}
	mustRPGLaunchSQL(t, fixture.database, `UPDATE game_variants SET status='BLOCKED',compatibility_code='VALIDATION_PENDING' WHERE game_id=?`, request.GameID)
	before := productValidationRows(t, reviewCheckpointFixture{database: fixture.database})
	cause := errors.New("product validation entropy unavailable")
	uuid.SetRand(playEntropyFailure{cause: cause})
	defer uuid.SetRand(nil)
	result, err := fixture.service.Create(t.Context(), "scummvm-profile", request)
	if !errors.Is(err, cause) || result.LaunchID != "" || result.JobID != "" {
		t.Fatalf("identity failure: launch=%q job=%q error=%v", result.LaunchID, result.JobID, err)
	}
	if actual := productValidationRows(t, reviewCheckpointFixture{database: fixture.database}); !reflect.DeepEqual(before, actual) {
		t.Fatal("entropy failure left job, input, event or variant writes")
	}
}

func TestProductCreationPreservesCancellation(t *testing.T) {
	fixture, request := productCreationFixture(t)
	before := playRows(t, fixture.database)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := fixture.launcher.Create(ctx, "local", request)
	if !errors.Is(err, context.Canceled) || result.LaunchID != "" {
		t.Fatalf("cancelled product create: launch=%q error=%v", result.LaunchID, err)
	}
	configDraftUnchanged(t, fixture, before)
}

func TestProductCreationPreservesSourceStorageCause(t *testing.T) {
	fixture, request := productCreationFixture(t)
	before := playRows(t, fixture.database)
	mustRPGLaunchSQL(t, fixture.database, `ALTER TABLE platform_instances RENAME TO unavailable_product_instances`)
	result, err := fixture.launcher.Create(t.Context(), "local", request)
	var storage *sqlite.Error
	if !errors.As(err, &storage) || result.LaunchID != "" {
		t.Fatalf("source failure: launch=%q error=%v", result.LaunchID, err)
	}
	configDraftUnchanged(t, fixture, before)
}

func TestProductCreationRechecksEnabledInstance(t *testing.T) {
	fixture, request := productCreationFixture(t)
	before := playRows(t, fixture.database)
	original := fixture.launcher.now
	invoked := false
	fixture.launcher.now = func() time.Time {
		if !invoked {
			invoked = true
			mustRPGLaunchSQL(t, fixture.database, `UPDATE platform_instances SET enabled=0 WHERE id=(SELECT platform_instance_id FROM games WHERE id=?)`, request.GameID)
		}
		return original()
	}
	result, err := fixture.launcher.Create(t.Context(), "local", request)
	if !invoked || !errors.Is(err, ErrBlocked) || result.LaunchID != "" {
		t.Fatalf("disabled instance: hook=%v launch=%q error=%v", invoked, result.LaunchID, err)
	}
	configDraftUnchanged(t, fixture, before)
}

const reviewScreenshotOverrideCode = "REVIEW_SCREENSHOT_OVERRIDE"

func newUUID() string { return uuid.Must(uuid.NewV7()).String() }
