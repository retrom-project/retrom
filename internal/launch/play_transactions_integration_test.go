//go:build integration

package launch

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	dbapi "retrom/internal/database"
	persistence "retrom/internal/persistence/launch"
	retromruntime "retrom/internal/runtime"
	application "retrom/internal/service/launch"
)

type failPlayAfterWork struct {
	repository application.PlayRepository
	failure    error
}

func (repository failPlayAfterWork) WithPlay(ctx context.Context, work func(application.PlayScope) error) error {
	return repository.repository.WithPlay(ctx, func(scope application.PlayScope) error {
		if err := work(scope); err != nil {
			return err
		}
		return repository.failure
	})
}

func playRows(t *testing.T, database dbapi.DB) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for _, table := range []string{
		"launch_sessions", "play_sessions", "runtime_preview_sessions",
		"launch_payload_retirements", "launch_game_save_bindings", "isolated_runtime_capabilities", "isolated_runtime_bootstrap_tickets", "save_states",
	} {
		result[table] = playTableRows(t, database, table)
	}
	return result
}

func playTableRows(t *testing.T, database dbapi.DB, table string) string {
	t.Helper()
	rows, err := database.QueryContext(t.Context(), "SELECT * FROM "+table+" ORDER BY 1,2")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Error(err)
		}
	}()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	records := make([][]any, 0)
	for rows.Next() {
		row := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range row {
			destinations[index] = &row[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			t.Fatal(err)
		}
		records = append(records, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func productPlayStart(t *testing.T, fixture reviewCheckpointFixture, created Created) {
	t.Helper()
	if _, err := fixture.launcher.RecordPlaySnapshot(t.Context(), created.LaunchID, created.Capability, PlaySnapshot{}); err != nil {
		t.Fatal(err)
	}
}

func seedPlayCapability(t *testing.T, fixture reviewCheckpointFixture, id string, preview bool) {
	t.Helper()
	var launchID, previewID *string
	if preview {
		previewID = &id
	} else {
		launchID = &id
	}
	mustRPGLaunchSQL(t, fixture.database, `INSERT INTO isolated_runtime_capabilities(credential_sha256,launch_id,preview_id,
 profile_id,expected_origin,issued_at_ms,expires_at_ms) VALUES(zeroblob(32),?,?,'local','http://play.localhost:3000',?,?)`,
		launchID, previewID, fixture.now.UnixMilli(), fixture.now.UnixMilli()+1_000_000)
}

func newPlaySourceFixture(t *testing.T, previewMode, activate bool) (reviewCheckpointFixture, Created) {
	t.Helper()
	if !previewMode {
		return newProductPlayFixture(t, activate)
	}
	fixture := newReviewCheckpointFixture(t)
	preview, err := fixture.launcher.CreateReviewPreview(t.Context(), ReviewPreviewRequest{
		ImportItemID: fixture.itemID, ActorUserID: "reviewer", IdempotencyKey: "play-preview",
	})
	if err != nil {
		t.Fatal(err)
	}
	if activate {
		if _, err := fixture.launcher.ReviewPreviewConfig(t.Context(), preview.PreviewID, preview.Capability); err != nil {
			t.Fatal(err)
		}
	}
	return fixture, Created{LaunchID: preview.PreviewID, Capability: preview.Capability}
}

func closeConfigSource(t *testing.T, fixture reviewCheckpointFixture, created Created, preview bool) {
	t.Helper()
	if preview {
		if err := fixture.launcher.FinishReviewPreview(t.Context(), created.LaunchID, created.Capability); err != nil {
			t.Fatal(err)
		}
		return
	}
	// Administrative revocation may race with config issuance; telemetry cannot revoke a product launch.
	mustRPGLaunchSQL(t, fixture.database, `UPDATE launch_sessions SET state='REVOKED',finished_at_ms=?,version=version+1 WHERE id=?`, fixture.now.UnixMilli(), created.LaunchID)
}

func TestPlaySnapshotTransactionRollsBack(t *testing.T) {
	for _, existing := range []bool{false, true} {
		fixture, created := newProductPlayFixture(t, true)
		if existing {
			productPlayStart(t, fixture, created)
		}
		before := playRows(t, fixture.database)
		cause := errors.New("late snapshot failure")
		c := application.NewPlayController(failPlayAfterWork{repository: persistence.NewPlay(fixture.database), failure: cause}, fixture.launcher.now, retromruntime.MatchesCapability)
		result, err := c.RecordSnapshot(t.Context(), created.LaunchID, created.Capability, PlaySnapshot{ActiveDurationMS: 1000})
		if !errors.Is(err, cause) || result.PlaySessionID != "" || !reflect.DeepEqual(before, playRows(t, fixture.database)) {
			t.Fatalf("snapshot partially committed: %#v %v", result, err)
		}
	}
}

func TestPreviewFinishRevokesCapabilityAndIsIdempotent(t *testing.T) {
	for _, active := range []bool{false, true} {
		fixture, created := newPlaySourceFixture(t, true, active)
		seedPlayCapability(t, fixture, created.LaunchID, true)
		if err := fixture.launcher.FinishReviewPreview(t.Context(), created.LaunchID, created.Capability); err != nil {
			t.Fatal(err)
		}
		before := playRows(t, fixture.database)
		if err := fixture.launcher.FinishReviewPreview(t.Context(), created.LaunchID, created.Capability); err != nil || !reflect.DeepEqual(before, playRows(t, fixture.database)) {
			t.Fatalf("repeat close: %v", err)
		}
		var revoked int64
		if err := dbapi.QueryRowContext(t.Context(), fixture.database, `SELECT revoked_at_ms FROM isolated_runtime_capabilities`).Scan(&revoked); err != nil || revoked != fixture.now.UnixMilli() {
			t.Fatalf("revoked=%d error=%v", revoked, err)
		}
		var plays int
		if err := dbapi.QueryRowContext(t.Context(), fixture.database, `SELECT count(*) FROM play_sessions`).Scan(&plays); err != nil || plays != 0 {
			t.Fatalf("preview statistics=%d error=%v", plays, err)
		}
	}
}

func TestPreviewFinishRejectsProductLaunchWithoutMutation(t *testing.T) {
	fixture, created := newProductPlayFixture(t, true)
	before := playRows(t, fixture.database)
	err := fixture.launcher.FinishReviewPreview(t.Context(), created.LaunchID, created.Capability)
	if !errors.Is(err, ErrCredential) || !reflect.DeepEqual(before, playRows(t, fixture.database)) {
		t.Fatalf("product closed: %v", err)
	}
}
