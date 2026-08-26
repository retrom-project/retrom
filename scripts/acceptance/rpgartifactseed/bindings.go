package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func promote(
	ctx context.Context,
	databasePath, statePath, oldGameID, oldSaveStateID string,
	now clock,
) (seedState, error) {
	if err := validatePaths(databasePath, statePath, false); err != nil {
		return seedState{}, err
	}
	if !uuidPattern.MatchString(oldGameID) || !uuidPattern.MatchString(oldSaveStateID) {
		return seedState{}, errors.New("ACC_RPG_012_OLD_PRODUCT_IDS_INVALID")
	}
	state, err := readState(statePath, databasePath)
	if err != nil {
		return seedState{}, err
	}
	if state.Phase != phaseOldSelected {
		return seedState{}, errors.New("ACC_RPG_012_PROMOTE_PHASE_INVALID")
	}
	database, err := openDatabase(ctx, databasePath, now)
	if err != nil {
		return seedState{}, err
	}
	defer func() { _ = database.close() }()
	checkpoint, err := loadCheckpoint(ctx, database.sql(), oldGameID, oldSaveStateID)
	if err != nil {
		return seedState{}, err
	}
	if checkpoint.ArtifactID != state.OldArtifact.ID || checkpoint.RouteKey != state.OldArtifact.RouteKey ||
		checkpoint.AdapterABI != state.OldArtifact.AdapterABI {
		return seedState{}, errors.New("ACC_RPG_012_OLD_CHECKPOINT_BINDING_MISMATCH")
	}
	instant := now().Truncate(time.Millisecond)
	if err := ensurePromotedSelection(ctx, database.sql(), state, instant); err != nil {
		return seedState{}, err
	}
	state.Phase = phaseNewSelected
	state.OldArtifact.SelectedForNewBindings = false
	state.OldArtifact.AvailableForLaunch = true
	state.NewArtifact.SelectedForNewBindings = true
	state.OldCheckpoint = &checkpoint
	state.UpdatedAtMS = instant.UnixMilli()
	if err := database.integrityCheck(ctx); err != nil {
		return seedState{}, err
	}
	if err := writeState(statePath, state); err != nil {
		return seedState{}, err
	}
	return state, nil
}

func ensurePromotedSelection(ctx context.Context, database *sql.DB, state seedState, now time.Time) error {
	oldSelected := verifySelectedArtifact(ctx, database, state.OldArtifact.ID) == nil
	newSelected := verifySelectedArtifact(ctx, database, state.NewArtifact.ID) == nil
	switch {
	case oldSelected && !newSelected:
		return selectArtifact(ctx, database, state.NewArtifact.ID, state.OldArtifact.ID, now)
	case !oldSelected && newSelected:
		return nil
	default:
		return errors.New("ACC_RPG_012_PROMOTE_SELECTION_STATE_INVALID")
	}
}

func loadCheckpoint(
	ctx context.Context,
	database *sql.DB,
	gameID, saveStateID string,
) (checkpointBinding, error) {
	var result checkpointBinding
	err := database.QueryRowContext(ctx, `
SELECT save.game_id,save.id,save.game_content_revision_id,save.game_variant_revision_id,
save.core_artifact_id,profile.route_key,save.adapter_abi,save.dependency_snapshot_sha256,
content.project_fingerprint
FROM save_states save
JOIN games game ON game.id=save.game_id AND game.status='PUBLISHED'
JOIN game_variant_revisions revision ON revision.id=save.game_variant_revision_id
 AND revision.game_content_revision_id=save.game_content_revision_id
 AND revision.core_artifact_id=save.core_artifact_id AND revision.status='READY'
JOIN rpgmaker_variant_profiles profile ON profile.game_variant_revision_id=revision.id
 AND profile.route_key=revision.route_key
 AND profile.adapter_abi=save.adapter_abi
 AND profile.dependency_snapshot_sha256=save.dependency_snapshot_sha256
JOIN rpgmaker_content_profiles content ON content.content_revision_id=revision.game_content_revision_id
JOIN core_artifacts artifact ON artifact.id=save.core_artifact_id
 AND artifact.runtime_family='RPGMAKER' AND artifact.available_for_launch=1
WHERE save.game_id=? AND save.id=? AND save.deleted_at_ms IS NULL
`, gameID, saveStateID).Scan(
		&result.GameID, &result.SaveStateID, &result.ContentRevisionID, &result.VariantRevisionID,
		&result.ArtifactID, &result.RouteKey, &result.AdapterABI, &result.DependencySnapshotSHA256,
		&result.ProjectFingerprint,
	)
	if err != nil {
		return checkpointBinding{}, fmt.Errorf("ACC_RPG_012_OLD_CHECKPOINT_NOT_PRODUCT_RESTORABLE: %w", err)
	}
	if !sha256Pattern.MatchString(result.DependencySnapshotSHA256) ||
		!sha256Pattern.MatchString(result.ProjectFingerprint) {
		return checkpointBinding{}, errors.New("ACC_RPG_012_OLD_CHECKPOINT_SNAPSHOT_INVALID")
	}
	packs, err := loadPacks(ctx, database, result.VariantRevisionID)
	if err != nil {
		return checkpointBinding{}, err
	}
	result.RuntimePacks = packs
	return result, nil
}

func loadVariant(ctx context.Context, database *sql.DB, gameID, coreID string) (variantBinding, error) {
	var result variantBinding
	err := database.QueryRowContext(ctx, `
SELECT game.id,revision.game_content_revision_id,revision.id,revision.core_artifact_id,
profile.route_key,profile.adapter_abi,profile.dependency_snapshot_sha256,
content.project_fingerprint
FROM games game
JOIN game_variants variant ON variant.game_id=game.id AND variant.core_id=?
JOIN game_variant_revisions revision ON revision.id=variant.current_revision_id
 AND revision.game_content_revision_id=game.current_content_revision_id AND revision.status='READY'
JOIN rpgmaker_variant_profiles profile ON profile.game_variant_revision_id=revision.id
JOIN rpgmaker_content_profiles content ON content.content_revision_id=revision.game_content_revision_id
JOIN core_artifacts artifact ON artifact.id=revision.core_artifact_id
 AND artifact.runtime_family='RPGMAKER' AND artifact.available_for_launch=1
WHERE game.id=? AND game.status='PUBLISHED'
`, coreID, gameID).Scan(
		&result.GameID, &result.ContentRevisionID, &result.VariantRevisionID, &result.ArtifactID,
		&result.RouteKey, &result.AdapterABI, &result.DependencySnapshotSHA256,
		&result.ProjectFingerprint,
	)
	if err != nil {
		return variantBinding{}, fmt.Errorf("ACC_RPG_012_NEW_VARIANT_NOT_READY: %w", err)
	}
	if !sha256Pattern.MatchString(result.DependencySnapshotSHA256) ||
		!sha256Pattern.MatchString(result.ProjectFingerprint) {
		return variantBinding{}, errors.New("ACC_RPG_012_NEW_VARIANT_SNAPSHOT_INVALID")
	}
	packs, err := loadPacks(ctx, database, result.VariantRevisionID)
	if err != nil {
		return variantBinding{}, err
	}
	result.RuntimePacks = packs
	return result, nil
}

func loadPacks(ctx context.Context, database *sql.DB, variantRevisionID string) ([]packBinding, error) {
	rows, err := database.QueryContext(ctx, `
SELECT reference.slot,reference.declared_name,reference.definition_id,
reference.installation_id,installation.files_digest
FROM game_variant_revision_runtime_packs reference
JOIN runtime_asset_pack_installations installation ON installation.id=reference.installation_id
WHERE reference.game_variant_revision_id=? ORDER BY reference.slot
`, variantRevisionID)
	if err != nil {
		return nil, fmt.Errorf("load runtime pack binding: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]packBinding, 0)
	for rows.Next() {
		var binding packBinding
		if err := rows.Scan(
			&binding.Slot, &binding.DeclaredName, &binding.DefinitionID,
			&binding.InstallationID, &binding.FilesDigest,
		); err != nil {
			return nil, fmt.Errorf("scan runtime pack binding: %w", err)
		}
		result = append(result, binding)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runtime pack binding: %w", err)
	}
	return result, nil
}
