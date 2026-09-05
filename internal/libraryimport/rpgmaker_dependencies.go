package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/rpgmaker/detector"
	"retrom/internal/rpgmaker/packs"
)

// RPG dependency decisions use the same pack resolver at import, recheck and
// publication. Trial sessions do not contribute to compatibility.
func resolveRPGDependencies(
	ctx context.Context, transaction *sql.Tx, profile rpgReviewBinding, selections []packs.Selection,
) (packs.Resolution, draftDependencyState, error) {
	generation := detector.Generation(profile.generation)
	definitions, installations, err := loadRPGPackCatalog(ctx, transaction, generation)
	if err != nil {
		return packs.Resolution{}, draftDependencyState{}, err
	}
	resolution, err := packs.Resolve(generation, profile.analysis.SelfContained, profile.override,
		profile.requirements, definitions, installations, selections)
	state := draftDependencyState{tracked: true, status: "READY", code: "READY"}
	switch {
	case err == nil:
	case errors.Is(err, packs.ErrMissing), errors.Is(err, packs.ErrAmbiguous), errors.Is(err, packs.ErrInvalid):
		state.status, state.code = "BLOCKED", err.Error()
		resolution.Bindings = []packs.Binding{}
	default:
		return packs.Resolution{}, draftDependencyState{}, fmt.Errorf("libraryimport/RPG pack resolution: %w", err)
	}
	snapshot, err := json.Marshal(map[string]any{"bindings": resolution.Bindings, "schemaVersion": 1})
	if err != nil {
		return packs.Resolution{}, draftDependencyState{}, fmt.Errorf("libraryimport/RPG dependencies: %w", err)
	}
	state.snapshotJSON = string(snapshot)
	return resolution, state, nil
}

func loadReviewRPGDependencies(
	ctx context.Context, transaction *sql.Tx, draftID string,
) (rpgReviewBinding, packs.Resolution, draftDependencyState, error) {
	profile, err := loadRPGReviewBinding(ctx, transaction, draftID)
	if err != nil {
		return profile, packs.Resolution{}, draftDependencyState{}, err
	}
	selections, err := loadReviewRPGSelections(ctx, transaction, draftID)
	if err != nil {
		return profile, packs.Resolution{}, draftDependencyState{}, err
	}
	resolution, state, err := resolveRPGDependencies(ctx, transaction, profile, selections)
	return profile, resolution, state, err
}

func loadReviewRPGSelections(ctx context.Context, transaction *sql.Tx, draftID string) ([]packs.Selection, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT slot,installation_id FROM review_draft_runtime_pack_selections
WHERE review_draft_id=? ORDER BY slot`, draftID)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/RPG selections: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	var selections []packs.Selection
	for rows.Next() {
		var selection packs.Selection
		if err := rows.Scan(&selection.Slot, &selection.InstallationID); err != nil {
			return nil, fmt.Errorf("libraryimport/RPG selection: %w", err)
		}
		selections = append(selections, selection)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("libraryimport/RPG selections: %w", err)
	}
	return selections, nil
}

func (run *creationRun) prepareRPGDependencies(record *groupRecord) error {
	profile := record.group.rpgProfile
	if profile == nil {
		return nil
	}
	binding := rpgReviewBinding{generation: string(profile.ExpectedGeneration)}
	binding.analysis.SelfContained = profile.SelfContained
	for _, requirement := range profile.RTPDependencies {
		binding.requirements = append(binding.requirements, packs.Requirement{
			Slot: requirement.Slot, DeclaredName: requirement.DeclaredName, NormalizedName: requirement.NormalizedName,
		})
	}
	resolution, state, err := resolveRPGDependencies(run.ctx, run.transaction, binding, nil)
	if err != nil {
		return err
	}
	record.group.rpgPackBindings = resolution.Bindings
	record.group.validationStatus, record.group.compatibilityCode = state.status, state.code
	record.group.dependencySnapshot = state.snapshotJSON
	return nil
}

func (state *draftValidationRefresh) resolveRPGDependencies() (draftDependencyState, error) {
	var draftID string
	if err := state.transaction.QueryRowContext(state.ctx,
		"SELECT id FROM review_drafts WHERE import_item_id=?", state.itemID).Scan(&draftID); err != nil {
		return draftDependencyState{}, ErrInvalid
	}
	profile, resolution, result, err := loadReviewRPGDependencies(state.ctx, state.transaction, draftID)
	if err != nil {
		return draftDependencyState{}, err
	}
	if result.status == "READY" {
		err = replaceRPGPackSelections(state.ctx, state.transaction, draftID,
			resolution.Bindings, profile.override, resolution.DependencySHA256, state.service.now().UnixMilli())
	}
	return result, err
}

func (service *Service) currentRPGReviewDependencies(
	ctx context.Context, validationID string, evidence reviewValidationEvidence,
) (bool, error) {
	transaction, err := service.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return false, fmt.Errorf("libraryimport/RPG current dependencies: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var draftID string
	err = transaction.QueryRowContext(ctx, `
SELECT draft.id FROM review_drafts draft
JOIN import_item_core_validations validation ON validation.import_item_id=draft.import_item_id
WHERE validation.id=?`, validationID).Scan(&draftID)
	if err != nil {
		return false, fmt.Errorf("libraryimport/RPG current draft: %w", err)
	}
	profile, resolution, current, err := loadReviewRPGDependencies(ctx, transaction, draftID)
	if err != nil {
		return false, err
	}
	return current.snapshotJSON == evidence.dependencyJSON && current.status == evidence.status &&
		current.code == evidence.compatibilityCode &&
		(current.status != "READY" || resolution.DependencySHA256 == profile.dependencySHA256), nil
}
