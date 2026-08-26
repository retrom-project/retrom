package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func inspect(ctx context.Context, databasePath, statePath string, now clock) (seedState, error) {
	if err := validatePaths(databasePath, statePath, false); err != nil {
		return seedState{}, err
	}
	state, err := readState(statePath, databasePath)
	if err != nil {
		return seedState{}, err
	}
	database, err := openDatabase(ctx, databasePath, now)
	if err != nil {
		return seedState{}, err
	}
	defer func() { _ = database.close() }()
	expected := state.OldArtifact.ID
	if state.Phase == phaseNewSelected || state.Phase == phaseDriftSeeded {
		expected = state.NewArtifact.ID
	}
	if err := verifySelectedArtifact(ctx, database.sql(), expected); err != nil {
		return seedState{}, err
	}
	if state.Phase == phaseDriftSeeded {
		if err := verifySeededState(ctx, database.sql(), state); err != nil {
			return seedState{}, err
		}
	}
	if err := database.integrityCheck(ctx); err != nil {
		return seedState{}, err
	}
	return state, nil
}

func verifySeededState(ctx context.Context, database *sql.DB, state seedState) error {
	if state.OldCheckpoint == nil || state.NewVariant == nil || state.DriftSaveStateIDs == nil {
		return errors.New("ACC_RPG_012_DRIFT_STATE_INCOMPLETE")
	}
	checks := []struct {
		id, contentID, artifactID, adapterABI, snapshot string
	}{
		{state.DriftSaveStateIDs.Content, state.NewVariant.ContentRevisionID, state.OldArtifact.ID,
			state.OldCheckpoint.AdapterABI, state.OldCheckpoint.DependencySnapshotSHA256},
		{state.DriftSaveStateIDs.Artifact, state.OldCheckpoint.ContentRevisionID, state.NewArtifact.ID,
			state.OldCheckpoint.AdapterABI, state.OldCheckpoint.DependencySnapshotSHA256},
		{state.DriftSaveStateIDs.Pack, state.OldCheckpoint.ContentRevisionID, state.OldArtifact.ID,
			state.OldCheckpoint.AdapterABI, ""},
		{state.DriftSaveStateIDs.AdapterABI, state.OldCheckpoint.ContentRevisionID, state.OldArtifact.ID,
			"acc-rpg-012-drift-abi", state.OldCheckpoint.DependencySnapshotSHA256},
	}
	for _, check := range checks {
		var contentID, artifactID, adapterABI, snapshot string
		err := database.QueryRowContext(ctx, `
SELECT game_content_revision_id,core_artifact_id,adapter_abi,dependency_snapshot_sha256
FROM save_states WHERE id=? AND game_id=? AND deleted_at_ms IS NULL
`, check.id, state.OldCheckpoint.GameID).Scan(&contentID, &artifactID, &adapterABI, &snapshot)
		if err != nil || contentID != check.contentID || artifactID != check.artifactID || adapterABI != check.adapterABI ||
			check.snapshot != "" && snapshot != check.snapshot || check.snapshot == "" && snapshot == state.OldCheckpoint.DependencySnapshotSHA256 {
			return fmt.Errorf("ACC_RPG_012_DRIFT_ROW_INVALID: %s", check.id)
		}
	}
	for _, trigger := range driftTriggers {
		var count int
		if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM sqlite_schema WHERE type='trigger' AND name=? AND sql IS NOT NULL
`, trigger).Scan(&count); err != nil || count != 1 {
			return fmt.Errorf("ACC_RPG_012_TRIGGER_NOT_RESTORED: %s", trigger)
		}
	}
	return nil
}
