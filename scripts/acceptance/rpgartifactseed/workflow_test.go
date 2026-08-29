package main

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"retrom/internal/store"
)

const (
	testOldRoute = "RPG2000_PREVIOUS_RELEASE"
	testNewRoute = "RPG2000_NEXT_RELEASE"
)

func TestPrepareBootstrapsRealArtifactsAndSelectsHistoricalRoute(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	options := testPrepareOptions(t)
	fixedNow := func() time.Time { return time.UnixMilli(1_700_000_000_000) }
	state, err := prepare(ctx, options, fixedNow)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if state.Phase != phaseOldSelected || !state.OldArtifact.SelectedForNewBindings ||
		state.NewArtifact.SelectedForNewBindings || state.OldArtifact.RouteKey != testOldRoute ||
		state.NewArtifact.RouteKey != testNewRoute {
		t.Fatalf("prepared state = %#v", state)
	}
	inspected, err := inspect(ctx, options.DatabasePath, options.StatePath, fixedNow)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if inspected.OldArtifact.ArtifactSetSHA256 == inspected.NewArtifact.ArtifactSetSHA256 {
		t.Fatal("old and new artifact sets are not distinct")
	}
}

func TestPrepareRejectsDatabaseWithBusinessData(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	options := testPrepareOptions(t)
	database, err := store.Open(ctx, options.DatabasePath, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.SQL.ExecContext(ctx, `
INSERT INTO upload_sessions(
 id,purpose,state,source_type,total_files,total_bytes,manifest_digest,
 expires_at_ms,created_at_ms,updated_at_ms)
VALUES('acceptance-non-fresh','GENERAL','CREATED','FILES',1,0,?,2,1,1)
`, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = prepare(ctx, options, time.Now)
	if err == nil || !strings.Contains(err.Error(), "ACC_RPG_012_DATABASE_NOT_FRESH") {
		t.Fatalf("prepare non-fresh error = %v", err)
	}
}

func TestPromoteRequiresActualProductCheckpoint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	options := testPrepareOptions(t)
	if _, err := prepare(ctx, options, time.Now); err != nil {
		t.Fatal(err)
	}
	_, err := promote(
		ctx, options.DatabasePath, options.StatePath,
		"5a2a7d29-50b9-4fe7-85c5-b2aeb00fa9d7", "e214b275-c2bb-48e3-a561-1da3b8237592", time.Now,
	)
	if err == nil || !strings.Contains(err.Error(), "ACC_RPG_012_OLD_CHECKPOINT_NOT_PRODUCT_RESTORABLE") {
		t.Fatalf("promote without checkpoint error = %v", err)
	}
}

func TestDriftIDsAreDeterministicAndSeparated(t *testing.T) {
	t.Parallel()
	saveID := "e214b275-c2bb-48e3-a561-1da3b8237592"
	first := deterministicDriftIDs(saveID)
	second := deterministicDriftIDs(saveID)
	if first != second {
		t.Fatalf("drift ids not deterministic: %#v != %#v", first, second)
	}
	unique := map[string]struct{}{
		first.Content: {}, first.Artifact: {}, first.Pack: {}, first.AdapterABI: {},
	}
	if len(unique) != 4 {
		t.Fatalf("drift ids not separated: %#v", first)
	}
}

func TestInsertDriftCheckpointsRestoresTriggersAndChangesOneBinding(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "drift.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	createDriftTestSchema(t, database)
	state, variant := driftTestBindings()
	drifts := deterministicDriftIDs(state.OldCheckpoint.SaveStateID)
	if err := insertDriftCheckpoints(
		ctx, database, state, variant, drifts, time.UnixMilli(1_700_000_000_000),
	); err != nil {
		t.Fatalf("insert drift checkpoints: %v", err)
	}
	state.DriftSaveStateIDs = &drifts
	state.NewVariant = &variant
	if err := verifySeededState(ctx, database, state); err != nil {
		t.Fatalf("verify seeded state: %v", err)
	}
	if err := ensureDriftCheckpoints(
		ctx, database, state, time.UnixMilli(1_700_000_000_001),
	); err != nil {
		t.Fatalf("resume committed drift seed: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO save_states(id,profile_id,game_id,game_content_revision_id,game_variant_revision_id,
core_artifact_id,adapter_abi,dependency_snapshot_sha256,payload_blob_id,payload_kind,payload_sha256,
payload_size_bytes,name,active_duration_ms,created_at_ms,updated_at_ms,source_launch_session_id)
VALUES('blocked','p','g','c','v','a','abi',?,'blob','RUNTIME_STATE',?,1,'blocked',0,1,1,'launch')
`, strings.Repeat("a", 64), strings.Repeat("b", 64)); err == nil {
		t.Fatal("restored insert trigger did not reject an unbound checkpoint")
	}
}

func TestPromoteSelectionResumesAfterCommittedDatabaseTransaction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	options := testPrepareOptions(t)
	state, err := prepare(ctx, options, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.Open(ctx, options.DatabasePath, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	instant := time.Now().Add(time.Second).Truncate(time.Millisecond)
	if err := selectArtifact(ctx, database.SQL, state.NewArtifact.ID, state.OldArtifact.ID, instant); err != nil {
		t.Fatal(err)
	}
	if err := ensurePromotedSelection(ctx, database.SQL, state, instant.Add(time.Millisecond)); err != nil {
		t.Fatalf("resume committed selection: %v", err)
	}
}

func TestStateIsBoundToDatabasePath(t *testing.T) {
	t.Parallel()
	options := testPrepareOptions(t)
	if _, err := prepare(context.Background(), options, time.Now); err != nil {
		t.Fatal(err)
	}
	otherDatabase := filepath.Join(t.TempDir(), "retrom.db")
	_, err := readState(options.StatePath, otherDatabase)
	if err == nil || !strings.Contains(err.Error(), "STATE_IDENTITY_INVALID") {
		t.Fatalf("read state for another database error = %v", err)
	}
}

func TestWorkflowErrorsHaveStableMachineCode(t *testing.T) {
	t.Parallel()
	if actual := workflowErrorCode(errors.New("wrap: ACC_RPG_012_DATABASE_NOT_FRESH: games=1")); actual != "ACC_RPG_012_DATABASE_NOT_FRESH" {
		t.Fatalf("specific code = %q", actual)
	}
	if actual := workflowErrorCode(errors.New("sqlite unavailable")); actual != "ACC_RPG_012_WORKFLOW_FAILED" {
		t.Fatalf("fallback code = %q", actual)
	}
}

func testPrepareOptions(t *testing.T) prepareOptions {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate repository")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	temporary := t.TempDir()
	return prepareOptions{
		DatabasePath:   filepath.Join(temporary, "retrom.db"),
		StatePath:      filepath.Join(temporary, "acc-rpg-012.json"),
		DependencyRoot: filepath.Join(repositoryRoot, "data"),
		Versions:       []string{"4.2.3", "4.3.0-pre"}, ActiveVersion: "4.2.3",
		CoreID: "rpgmaker_2000", OldRoute: testOldRoute, NewRoute: testNewRoute,
		Acknowledgment: caseID,
	}
}

func createDriftTestSchema(t *testing.T, database *sql.DB) {
	t.Helper()
	_, err := database.Exec(`
CREATE TABLE save_states(
 id TEXT PRIMARY KEY,profile_id TEXT,game_id TEXT,game_content_revision_id TEXT,
 game_variant_revision_id TEXT,core_artifact_id TEXT,adapter_abi TEXT,
 dependency_snapshot_sha256 TEXT,dat_version_id TEXT,dos_entry_path TEXT,payload_blob_id TEXT,
 payload_kind TEXT,native_profile TEXT,resume_slot INTEGER,payload_sha256 TEXT,payload_size_bytes INTEGER,
 screenshot_blob_id TEXT,name TEXT,active_duration_ms INTEGER,version INTEGER DEFAULT 1,
 created_at_ms INTEGER,updated_at_ms INTEGER,deleted_at_ms INTEGER,source_launch_session_id TEXT,disc_index INTEGER);
CREATE TRIGGER save_states_source_launch_insert BEFORE INSERT ON save_states
WHEN NEW.id<>'source' BEGIN SELECT RAISE(ABORT,'source mismatch'); END;
CREATE TRIGGER save_states_payload_insert BEFORE INSERT ON save_states
WHEN NEW.id<>'source' BEGIN SELECT RAISE(ABORT,'payload mismatch'); END;
INSERT INTO save_states(
 id,profile_id,game_id,game_content_revision_id,game_variant_revision_id,core_artifact_id,
 adapter_abi,dependency_snapshot_sha256,payload_blob_id,payload_kind,payload_sha256,payload_size_bytes,
 name,active_duration_ms,created_at_ms,updated_at_ms,source_launch_session_id)
VALUES('source','profile','11111111-1111-4111-8111-111111111111','old-content','old-variant',
 '22222222-2222-4222-8222-222222222222','old-abi',
 '` + strings.Repeat("c", 64) + `','blob','RUNTIME_STATE','` + strings.Repeat("d", 64) + `',10,
 'source',0,1,1,'launch');
`)
	if err != nil {
		t.Fatal(err)
	}
}

func driftTestBindings() (seedState, variantBinding) {
	state := seedState{
		OldArtifact: artifactBinding{ID: "22222222-2222-4222-8222-222222222222"},
		NewArtifact: artifactBinding{ID: "33333333-3333-4333-8333-333333333333"},
		OldCheckpoint: &checkpointBinding{
			GameID: "11111111-1111-4111-8111-111111111111", SaveStateID: "source",
			ContentRevisionID: "old-content", VariantRevisionID: "old-variant",
			ArtifactID: "22222222-2222-4222-8222-222222222222", AdapterABI: "old-abi",
			ProjectFingerprint:       strings.Repeat("a", 64),
			DependencySnapshotSHA256: strings.Repeat("c", 64),
		},
	}
	variant := variantBinding{
		GameID: "44444444-4444-4444-8444-444444444444", ContentRevisionID: "new-content",
		VariantRevisionID: "new-variant", ArtifactID: state.NewArtifact.ID,
		ProjectFingerprint: strings.Repeat("b", 64),
		AdapterABI:         "new-abi", DependencySnapshotSHA256: strings.Repeat("e", 64),
	}
	return state, variant
}
