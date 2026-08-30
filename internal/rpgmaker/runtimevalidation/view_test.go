package runtimevalidation

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	rpgvalidation "retrom/internal/rpgmaker/validation"
)

func TestGetReconcilesValidationWhoseGameWindowAlreadyClosed(t *testing.T) {
	t.Parallel()
	database := openViewTestDatabase(t)
	machineGates, err := initialMachineGates()
	if err != nil {
		t.Fatal(err)
	}
	const (
		itemID       = "00000000-0000-4000-8000-000000000001"
		validationID = "00000000-0000-4000-8000-000000000002"
		launchID     = "00000000-0000-4000-8000-000000000003"
	)
	if _, err := database.ExecContext(
		context.Background(), `INSERT INTO launch_sessions(id,state) VALUES(?,'FINISHED')`, launchID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(context.Background(), `
INSERT INTO rpgmaker_runtime_validations(
 id,import_item_id,review_version_at_create,runtime_binding_revision,
 effective_source_snapshot_id,project_fingerprint,core_id,generation,
 evidence_confidence,route_key,artifact_id,artifact_set_sha256,adapter_id,adapter_abi,
 dependency_snapshot_sha256,launch_id,state,last_gate_sequence,machine_gates_json,
 created_at_ms,updated_at_ms,expires_at_ms
) VALUES(
 ?,?,1,1,'snapshot','fingerprint','rpgmaker_2000','RPG2000','FAMILY_ONLY','easyrpg',
 'artifact','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
 'easyrpg-web','easyrpg-save','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
 ?,'STARTING',0,?,1000,1001,2000
)`, validationID, itemID, launchID, machineGates); err != nil {
		t.Fatal(err)
	}

	view, err := New(database, nil, func() time.Time { return time.UnixMilli(1500) }).
		Get(context.Background(), itemID, validationID)
	if err != nil {
		t.Fatal(err)
	}
	if view.State != "FAILED" || view.FailureCode == nil ||
		*view.FailureCode != "RPG_RUNTIME_VALIDATION_WINDOW_CLOSED" {
		t.Fatalf("reconciled validation = %#v", view)
	}
}

func TestLoadViewAcceptsFailedValidationBeforeLaunch(t *testing.T) {
	t.Parallel()
	database := openViewTestDatabase(t)
	machineGates, err := initialMachineGates()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(context.Background(), `
INSERT INTO rpgmaker_runtime_validations(
 id,import_item_id,review_version_at_create,runtime_binding_revision,
 effective_source_snapshot_id,project_fingerprint,core_id,generation,
 evidence_generation,evidence_confidence,route_key,artifact_id,artifact_set_sha256,
 adapter_id,adapter_abi,dependency_snapshot_sha256,launch_id,restore_launch_id,state,
 last_gate_sequence,machine_gates_json,failure_code,created_at_ms,updated_at_ms,expires_at_ms
) VALUES(
 'validation','item',1,1,'snapshot','fingerprint','rpgmaker_2000','RPG2000',
 NULL,'FAMILY_ONLY','easyrpg','artifact','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
 'easyrpg-web','easyrpg-save','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
 NULL,NULL,'FAILED',0,?,'RPG_RUNTIME_ROUTE_UNAVAILABLE',1000,1001,2000
)`, machineGates); err != nil {
		t.Fatal(err)
	}
	transaction, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = transaction.Rollback() })

	view, err := loadView(context.Background(), transaction, "item", "validation")
	if err != nil {
		t.Fatal(err)
	}
	if view.LaunchID != nil {
		t.Fatalf("launchId = %q, want null", *view.LaunchID)
	}
	if view.State != "FAILED" || view.FailureCode == nil || *view.FailureCode != "RPG_RUNTIME_ROUTE_UNAVAILABLE" {
		t.Fatalf("view = %#v", view)
	}
}

func TestLoadViewRejectsNonFailedValidationWithoutLaunch(t *testing.T) {
	t.Parallel()
	database := openViewTestDatabase(t)
	machineGates, err := initialMachineGates()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(context.Background(), `
INSERT INTO rpgmaker_runtime_validations(
 id,import_item_id,review_version_at_create,runtime_binding_revision,
 effective_source_snapshot_id,project_fingerprint,core_id,generation,
 evidence_confidence,route_key,artifact_id,artifact_set_sha256,adapter_id,adapter_abi,
	 dependency_snapshot_sha256,state,last_gate_sequence,
 machine_gates_json,created_at_ms,updated_at_ms,expires_at_ms
) VALUES(
 'validation','item',1,1,'snapshot','fingerprint','rpgmaker_2000','RPG2000',
 'FAMILY_ONLY','easyrpg','artifact','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
 'easyrpg-web','easyrpg-save','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
 'CREATED',0,?,1000,1001,2000
)`, machineGates); err != nil {
		t.Fatal(err)
	}
	transaction, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = transaction.Rollback() })

	_, err = loadView(context.Background(), transaction, "item", "validation")
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("error = %v, want %v", err, ErrInvalidState)
	}
}

func TestCheckpointRoundTripProjectsInitialRestoreAndPostRestoreInputEvidence(t *testing.T) {
	t.Parallel()
	original, restore := "original-launch", "restore-launch"
	row := viewRow{launchID: sql.NullString{String: original, Valid: true}, restoreID: sql.NullString{String: restore, Valid: true}}
	result := CheckpointRoundTrip{OriginalLaunchID: &original, RestoreLaunchID: &restore}
	initial := rpgPosition(1, 0)
	saved := rpgPosition(2, 1)
	diverged := rpgPosition(3, 2)
	input := rpgPosition(4, 3)
	for _, event := range []storedEvent{
		{Gate: "INITIAL_POSITION_RECORDED", Position: initial},
		{Gate: "SAVE_POINT_RECORDED", Position: saved},
		{Gate: "POST_SAVE_STATE_DIVERGED", Position: diverged},
		{Gate: "RESTORE_POSITION_VERIFIED", Position: saved},
		{Gate: "RESTORE_INPUT", Position: input},
	} {
		applyCheckpointEvent(&result, event, row)
	}
	if !result.PositionVerified || !result.RestoreInputVerified || result.InitialPosition == nil ||
		result.RestoreInputPosition == nil || *result.OriginalLaunchID != original ||
		*result.RestoreLaunchID != restore {
		t.Fatalf("round trip = %#v", result)
	}
}

func rpgPosition(playerX, fixtureState int64) *rpgvalidation.Position {
	return &rpgvalidation.Position{
		MapID: 1, PlayerX: playerX, PlayerY: 1, FixtureState: fixtureState,
	}
}

func openViewTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.ExecContext(context.Background(), `
CREATE TABLE rpgmaker_runtime_validations(
 id TEXT PRIMARY KEY,import_item_id TEXT,review_version_at_create INTEGER,
 runtime_binding_revision INTEGER,effective_source_snapshot_id TEXT,
 project_fingerprint TEXT,core_id TEXT,generation TEXT,evidence_generation TEXT,
 evidence_confidence TEXT,route_key TEXT,artifact_id TEXT,artifact_set_sha256 TEXT,
 adapter_id TEXT,adapter_abi TEXT,dependency_snapshot_sha256 TEXT,launch_id TEXT,
 restore_launch_id TEXT,state TEXT,last_gate_sequence INTEGER,machine_gates_json TEXT,
 failure_code TEXT,decision_note TEXT,decided_by_user_id TEXT,decided_at_ms INTEGER,
 evidence_screenshot_blob_id TEXT,created_at_ms INTEGER,updated_at_ms INTEGER,expires_at_ms INTEGER
);
CREATE TABLE rpgmaker_runtime_validation_checkpoints(
 validation_id TEXT,payload_kind TEXT,resume_slot INTEGER,size_bytes INTEGER,payload_sha256 TEXT
);
CREATE TABLE rpgmaker_runtime_validation_gate_events(
 validation_id TEXT,sequence INTEGER,event_id TEXT,launch_id TEXT,gate TEXT,phase TEXT,
 observed_at_ms INTEGER,evidence_json TEXT
);
CREATE TABLE launch_sessions(id TEXT PRIMARY KEY,state TEXT
);`); err != nil {
		t.Fatal(err)
	}
	return database
}
