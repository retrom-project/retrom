package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var driftTriggers = []string{"save_states_source_launch_insert", "save_states_payload_insert"}

func seedDrift(
	ctx context.Context,
	databasePath, statePath, newGameID string,
	now clock,
) (seedState, error) {
	if err := validatePaths(databasePath, statePath, false); err != nil {
		return seedState{}, err
	}
	if !uuidPattern.MatchString(newGameID) {
		return seedState{}, errors.New("ACC_RPG_012_NEW_GAME_ID_INVALID")
	}
	state, err := readState(statePath, databasePath)
	if err != nil {
		return seedState{}, err
	}
	if state.Phase != phaseNewSelected || state.OldCheckpoint == nil {
		return seedState{}, errors.New("ACC_RPG_012_DRIFT_PHASE_INVALID")
	}
	if newGameID == state.OldCheckpoint.GameID {
		return seedState{}, errors.New("ACC_RPG_012_NEW_GAME_MUST_BE_DISTINCT")
	}
	database, err := openDatabase(ctx, databasePath, now)
	if err != nil {
		return seedState{}, err
	}
	defer func() { _ = database.close() }()
	if err := verifySelectedArtifact(ctx, database.sql(), state.NewArtifact.ID); err != nil {
		return seedState{}, err
	}
	variant, err := loadVariant(ctx, database.sql(), newGameID, state.NewArtifact.CoreID)
	if err != nil {
		return seedState{}, err
	}
	if variant.ArtifactID != state.NewArtifact.ID || variant.RouteKey != state.NewArtifact.RouteKey ||
		variant.ContentRevisionID == state.OldCheckpoint.ContentRevisionID ||
		variant.ProjectFingerprint == state.OldCheckpoint.ProjectFingerprint {
		return seedState{}, errors.New("ACC_RPG_012_NEW_VARIANT_BINDING_MISMATCH")
	}
	drifts := deterministicDriftIDs(state.OldCheckpoint.SaveStateID)
	instant := now().Truncate(time.Millisecond)
	candidate := state
	candidate.NewVariant = &variant
	candidate.DriftSaveStateIDs = &drifts
	if err := ensureDriftCheckpoints(ctx, database.sql(), candidate, instant); err != nil {
		return seedState{}, err
	}
	state.Phase = phaseDriftSeeded
	state.NewVariant = &variant
	state.DriftSaveStateIDs = &drifts
	state.UpdatedAtMS = instant.UnixMilli()
	if err := database.integrityCheck(ctx); err != nil {
		return seedState{}, err
	}
	if err := verifySeededState(ctx, database.sql(), state); err != nil {
		return seedState{}, err
	}
	if err := writeState(statePath, state); err != nil {
		return seedState{}, err
	}
	return state, nil
}

func ensureDriftCheckpoints(
	ctx context.Context,
	database *sql.DB,
	state seedState,
	now time.Time,
) error {
	drifts := state.DriftSaveStateIDs
	if drifts == nil || state.NewVariant == nil {
		return errors.New("ACC_RPG_012_DRIFT_STATE_INCOMPLETE")
	}
	var count int
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM save_states WHERE id IN (?,?,?,?)
`, drifts.Content, drifts.Artifact, drifts.Pack, drifts.AdapterABI).Scan(&count); err != nil {
		return fmt.Errorf("count drift checkpoints: %w", err)
	}
	if count == 4 {
		return verifySeededState(ctx, database, state)
	}
	if count != 0 {
		return errors.New("ACC_RPG_012_PARTIAL_DRIFT_STATE")
	}
	return insertDriftCheckpoints(ctx, database, state, *state.NewVariant, *drifts, now)
}

func deterministicDriftIDs(saveStateID string) driftBinding {
	prefix := "retrom:acceptance:acc-rpg-012:" + saveStateID + ":"
	return driftBinding{
		Content:    uuid.NewSHA1(uuid.NameSpaceURL, []byte(prefix+"content")).String(),
		Artifact:   uuid.NewSHA1(uuid.NameSpaceURL, []byte(prefix+"artifact")).String(),
		Pack:       uuid.NewSHA1(uuid.NameSpaceURL, []byte(prefix+"pack")).String(),
		AdapterABI: uuid.NewSHA1(uuid.NameSpaceURL, []byte(prefix+"adapter-abi")).String(),
	}
}

func insertDriftCheckpoints(
	ctx context.Context,
	database *sql.DB,
	state seedState,
	variant variantBinding,
	drifts driftBinding,
	now time.Time,
) error {
	triggerSQL, err := loadTriggerSQL(ctx, database)
	if err != nil {
		return err
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin drift seed: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	for _, name := range driftTriggers {
		if _, err := transaction.ExecContext(ctx, "DROP TRIGGER "+name); err != nil {
			return fmt.Errorf("drop acceptance tamper trigger %s: %w", name, err)
		}
	}
	packDigest := sha256.Sum256([]byte(caseID + "\x00PACK\x00" + state.OldCheckpoint.SaveStateID))
	mutations := []checkpointMutation{
		{drifts.Content, variant.ContentRevisionID, state.OldArtifact.ID, state.OldCheckpoint.AdapterABI,
			state.OldCheckpoint.DependencySnapshotSHA256, "content"},
		{drifts.Artifact, state.OldCheckpoint.ContentRevisionID, state.NewArtifact.ID, state.OldCheckpoint.AdapterABI,
			state.OldCheckpoint.DependencySnapshotSHA256, "artifact"},
		{drifts.Pack, state.OldCheckpoint.ContentRevisionID, state.OldArtifact.ID, state.OldCheckpoint.AdapterABI,
			hex.EncodeToString(packDigest[:]), "pack"},
		{drifts.AdapterABI, state.OldCheckpoint.ContentRevisionID, state.OldArtifact.ID,
			"acc-rpg-012-drift-abi", state.OldCheckpoint.DependencySnapshotSHA256, "adapter ABI"},
	}
	for _, mutation := range mutations {
		if err := cloneCheckpoint(ctx, transaction, state.OldCheckpoint.SaveStateID, mutation, now); err != nil {
			return err
		}
	}
	for _, name := range driftTriggers {
		if _, err := transaction.ExecContext(ctx, triggerSQL[name]); err != nil {
			return fmt.Errorf("restore acceptance tamper trigger %s: %w", name, err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit drift seed: %w", err)
	}
	return nil
}

type checkpointMutation struct {
	id, contentID, artifactID, adapterABI, snapshot, label string
}

func cloneCheckpoint(
	ctx context.Context,
	transaction *sql.Tx,
	sourceID string,
	mutation checkpointMutation,
	now time.Time,
) error {
	result, err := transaction.ExecContext(ctx, `
INSERT INTO save_states(
 id,profile_id,game_id,game_content_revision_id,game_variant_revision_id,core_artifact_id,
 adapter_abi,dependency_snapshot_sha256,dat_version_id,dos_entry_path,payload_blob_id,payload_kind,
 native_profile,resume_slot,payload_sha256,payload_size_bytes,screenshot_blob_id,name,
 active_duration_ms,version,created_at_ms,updated_at_ms,deleted_at_ms,source_launch_session_id,disc_index)
SELECT ?,profile_id,game_id,?,game_variant_revision_id,?,?,?,dat_version_id,dos_entry_path,
 payload_blob_id,payload_kind,native_profile,resume_slot,payload_sha256,payload_size_bytes,
 screenshot_blob_id,?,active_duration_ms,1,?,?,NULL,source_launch_session_id,disc_index
FROM save_states WHERE id=? AND deleted_at_ms IS NULL
`, mutation.id, mutation.contentID, mutation.artifactID, mutation.adapterABI, mutation.snapshot,
		"ACC-RPG-012 tamper: "+mutation.label, now.UnixMilli(), now.UnixMilli(), sourceID)
	if err != nil {
		return fmt.Errorf("seed %s drift checkpoint: %w", mutation.label, err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("ACC_RPG_012_SOURCE_CHECKPOINT_MISSING: %s", mutation.label)
	}
	return nil
}

func loadTriggerSQL(ctx context.Context, database *sql.DB) (map[string]string, error) {
	result := make(map[string]string, len(driftTriggers))
	for _, name := range driftTriggers {
		var statement string
		if err := database.QueryRowContext(ctx, `
SELECT sql FROM sqlite_schema WHERE type='trigger' AND name=?
`, name).Scan(&statement); err != nil || statement == "" {
			return nil, fmt.Errorf("ACC_RPG_012_REQUIRED_TRIGGER_MISSING: %s", name)
		}
		result[name] = statement
	}
	return result, nil
}
