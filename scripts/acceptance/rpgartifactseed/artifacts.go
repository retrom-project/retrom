package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type prepareOptions struct {
	DatabasePath   string
	StatePath      string
	DependencyRoot string
	Versions       []string
	ActiveVersion  string
	CoreID         string
	OldRoute       string
	NewRoute       string
	Acknowledgment string
}

func prepare(ctx context.Context, options prepareOptions, now clock) (seedState, error) {
	if options.Acknowledgment != caseID {
		return seedState{}, errors.New("ACC_RPG_012_FRESH_DATABASE_ACK_REQUIRED")
	}
	if err := validatePaths(options.DatabasePath, options.StatePath, true); err != nil {
		return seedState{}, err
	}
	if options.CoreID == "" || options.OldRoute == "" || options.NewRoute == "" || options.OldRoute == options.NewRoute {
		return seedState{}, errors.New("ACC_RPG_012_ARTIFACT_ROUTES_INVALID")
	}
	database, err := openDatabase(ctx, options.DatabasePath, now)
	if err != nil {
		return seedState{}, err
	}
	defer func() { _ = database.close() }()
	if err := requireFreshBusinessDatabase(ctx, database.sql()); err != nil {
		return seedState{}, err
	}
	instant := now().Truncate(time.Millisecond)
	if err := bootstrapDependencies(
		ctx, database.sql(), options.DependencyRoot, options.Versions, options.ActiveVersion, instant,
	); err != nil {
		return seedState{}, err
	}
	oldArtifact, err := loadArtifact(ctx, database.sql(), options.CoreID, options.OldRoute)
	if err != nil {
		return seedState{}, fmt.Errorf("load old artifact: %w", err)
	}
	newArtifact, err := loadArtifact(ctx, database.sql(), options.CoreID, options.NewRoute)
	if err != nil {
		return seedState{}, fmt.Errorf("load new artifact: %w", err)
	}
	if oldArtifact.ID == newArtifact.ID || oldArtifact.Generation != newArtifact.Generation ||
		oldArtifact.ArtifactSetSHA256 == newArtifact.ArtifactSetSHA256 {
		return seedState{}, errors.New("ACC_RPG_012_ARTIFACT_PAIR_NOT_DISTINCT")
	}
	if err := selectArtifact(ctx, database.sql(), oldArtifact.ID, newArtifact.ID, instant); err != nil {
		return seedState{}, err
	}
	oldArtifact.SelectedForNewBindings = true
	newArtifact.SelectedForNewBindings = false
	state := seedState{
		SchemaVersion: stateVersion, CaseID: caseID, Phase: phaseOldSelected,
		DatabasePathSHA256: databasePathDigest(options.DatabasePath),
		OldArtifact:        oldArtifact, NewArtifact: newArtifact, UpdatedAtMS: instant.UnixMilli(),
	}
	if err := database.integrityCheck(ctx); err != nil {
		return seedState{}, err
	}
	if err := writeState(options.StatePath, state); err != nil {
		return seedState{}, err
	}
	return state, nil
}

func loadArtifact(ctx context.Context, database *sql.DB, coreID, routeKey string) (artifactBinding, error) {
	var result artifactBinding
	var selected, available int
	err := database.QueryRowContext(ctx, `
SELECT artifact.id,artifact.core_id,mapping.generation,artifact.route_key,
artifact.artifact_set_sha256,artifact.adapter_id,
json_extract(artifact.compatibility_json,'$.adapterAbi'),artifact.manifest_sha256,
artifact.selected_for_new_bindings,artifact.available_for_launch
FROM core_artifacts artifact
JOIN rpgmaker_core_generations mapping ON mapping.core_id=artifact.core_id
WHERE artifact.core_id=? AND artifact.route_key=? AND artifact.runtime_family='RPGMAKER'
`, coreID, routeKey).Scan(
		&result.ID, &result.CoreID, &result.Generation, &result.RouteKey,
		&result.ArtifactSetSHA256, &result.AdapterID, &result.AdapterABI, &result.ManifestSHA256,
		&selected, &available,
	)
	if err != nil {
		return artifactBinding{}, err
	}
	result.SelectedForNewBindings = selected == 1
	result.AvailableForLaunch = available == 1
	if err := validateArtifact(result); err != nil {
		return artifactBinding{}, err
	}
	return result, nil
}

func selectArtifact(ctx context.Context, database *sql.DB, selectedID, deselectedID string, now time.Time) error {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin artifact selection: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	if _, err := transaction.ExecContext(ctx, `
UPDATE core_artifacts SET selected_for_new_bindings=0,version=version+1,updated_at_ms=?
WHERE id=? AND selected_for_new_bindings<>0
`, now.UnixMilli(), deselectedID); err != nil {
		return fmt.Errorf("deselect artifact: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE core_artifacts SET selected_for_new_bindings=1,available_for_launch=1,
version=version+1,updated_at_ms=? WHERE id=? AND runtime_family='RPGMAKER'
`, now.UnixMilli(), selectedID)
	if err != nil {
		return fmt.Errorf("select artifact: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return errors.New("ACC_RPG_012_ARTIFACT_SELECTION_TARGET_MISSING")
	}
	if err := verifySelectedArtifact(ctx, transaction, selectedID); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit artifact selection: %w", err)
	}
	return nil
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func verifySelectedArtifact(ctx context.Context, database queryer, expectedID string) error {
	var selectedID string
	err := database.QueryRowContext(ctx, `
SELECT selected.id FROM core_artifacts selected
WHERE selected.id=? AND selected.selected_for_new_bindings=1 AND selected.available_for_launch=1
AND NOT EXISTS(SELECT 1 FROM core_artifacts other
 WHERE other.core_id=selected.core_id AND other.selected_for_new_bindings=1 AND other.id<>selected.id)
`, expectedID).Scan(&selectedID)
	if err != nil || selectedID != expectedID {
		return errors.New("ACC_RPG_012_SELECTED_ARTIFACT_INVARIANT_FAILED")
	}
	return nil
}
