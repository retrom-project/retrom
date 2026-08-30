//go:build integration

package launch

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/cleanup"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/testsupport"
)

func TestRPGValidationWindowCloseFailsUnfinishedValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	now := time.UnixMilli(1_786_100_000_000)
	database, err := testsupport.OpenDatabase(
		ctx, filepath.Join(dataDir, "retrom.db"), func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	seedLocalProfile(t, database.SQL)
	fixture := seedRPGValidationLaunchFixture(t, database.SQL, now.UnixMilli())
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	service := New(database.SQL, nil, credentials, func() time.Time { return now })
	created, err := service.CreateRPGValidation(
		ctx, "local", fixture.validationID, "/admin/reviews/"+fixture.itemID, Capabilities{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Config(ctx, created.LaunchID, created.Capability); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordPlay(ctx, created.LaunchID, created.Capability, "start", PlayEvent{
		ClientSequence: 0, ClientObservedAtMS: now.UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	mustRPGLaunchSQL(t, database.SQL, `
UPDATE rpgmaker_runtime_validations SET state='RUNNING',updated_at_ms=? WHERE id=?`,
		now.UnixMilli(), fixture.validationID)
	if _, err := service.RecordPlay(ctx, created.LaunchID, created.Capability, "finish", PlayEvent{
		ClientSequence: 1, ClientObservedAtMS: now.UnixMilli() + 1,
		PreviousInterval: &Interval{Running: true, Visible: true},
	}); err != nil {
		t.Fatal(err)
	}

	assertClosedRPGValidation(t, database.SQL, fixture.validationID)
}

func assertClosedRPGValidation(t *testing.T, database *sql.DB, validationID string) {
	t.Helper()
	var state string
	var failureCode sql.NullString
	if err := database.QueryRow(`
SELECT state,failure_code FROM rpgmaker_runtime_validations WHERE id=?`, validationID).
		Scan(&state, &failureCode); err != nil {
		t.Fatal(err)
	}
	if state != "FAILED" || failureCode.String != "RPG_RUNTIME_VALIDATION_WINDOW_CLOSED" {
		t.Fatalf("closed RPG validation = (%q, %#v), want FAILED/window-closed", state, failureCode)
	}
}
