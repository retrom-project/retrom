package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/rpgmaker/detector"
)

// Import, review refresh and approval evaluate the same project-owned resource
// policy. An explicit administrator confirmation can override external RTP declarations;
// installed packs and trial sessions never contribute resources or compatibility.
func resolveRPGDependencies(profile rpgReviewBinding) (draftDependencyState, string) {
	requirements := detector.ExternalRTPRequirements(detector.Generation(profile.generation),
		profile.analysis.SelfContained, profile.analysis.Requirements.RTP)
	state := draftDependencyState{tracked: true, status: "READY", code: "READY"}
	if len(requirements) > 0 && !profile.override {
		state.status, state.code = "BLOCKED", "RPG_EXTERNAL_RTP_REQUIRED"
	}
	snapshot, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "policy": "PROJECT_RESOURCES_ONLY", "externalRTP": requirements,
		"selfContainedOverride": profile.override,
	})
	state.snapshotJSON = string(snapshot)
	digest := sha256.Sum256(snapshot)
	return state, hex.EncodeToString(digest[:])
}

func loadReviewRPGDependencies(ctx context.Context, transaction *sql.Tx, draftID string) (
	rpgReviewBinding, string, draftDependencyState, error,
) {
	profile, err := loadRPGReviewBinding(ctx, transaction, draftID)
	if err != nil {
		return profile, "", draftDependencyState{}, err
	}
	state, digest := resolveRPGDependencies(profile)
	return profile, digest, state, nil
}

func (run *creationRun) prepareRPGDependencies(record *groupRecord) error {
	profile := record.group.rpgProfile
	if profile == nil {
		return nil
	}
	binding := rpgReviewBinding{generation: string(profile.ExpectedGeneration)}
	binding.analysis.SelfContained = profile.SelfContained
	binding.analysis.Requirements.RTP = profile.RTPDependencies
	state, _ := resolveRPGDependencies(binding)
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
	_, digest, result, err := loadReviewRPGDependencies(state.ctx, state.transaction, draftID)
	if err != nil {
		return draftDependencyState{}, err
	}
	if _, err := state.transaction.ExecContext(state.ctx, `
UPDATE rpgmaker_review_profiles SET dependency_snapshot_sha256=?,updated_at_ms=? WHERE review_draft_id=?
`, digest, state.service.now().UnixMilli(), draftID); err != nil {
		return draftDependencyState{}, fmt.Errorf("libraryimport/RPG dependency digest: %w", err)
	}
	return result, nil
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
	if err := transaction.QueryRowContext(ctx, `
SELECT draft.id FROM review_drafts draft
JOIN import_item_core_validations validation ON validation.import_item_id=draft.import_item_id
WHERE validation.id=?`, validationID).Scan(&draftID); err != nil {
		return false, fmt.Errorf("libraryimport/RPG current draft: %w", err)
	}
	profile, digest, current, err := loadReviewRPGDependencies(ctx, transaction, draftID)
	if err != nil {
		return false, err
	}
	return current.snapshotJSON == evidence.dependencyJSON && current.status == evidence.status &&
		current.code == evidence.compatibilityCode && digest == profile.dependencySHA256, nil
}
