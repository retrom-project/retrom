package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/service/libraryimport"
)

// Import, review refresh and approval evaluate the same project-owned resource
// policy. An explicit administrator confirmation can override external RTP declarations;
// installed packs and trial sessions never contribute resources or compatibility.
func resolveRPGDependencies(profile rpgReviewBinding) (draftDependencyState, string) {
	resolved := application.ResolveRPGResourcePolicy(profile.generation, profile.override, profile.analysis)
	state := draftDependencyState{
		tracked:      true,
		status:       resolved.Status,
		code:         resolved.Code,
		snapshotJSON: resolved.SnapshotJSON,
	}
	return state, resolved.Digest
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
	profile := record.group.RPGProfile
	if profile == nil {
		return nil
	}
	binding := rpgReviewBinding{generation: string(profile.ExpectedGeneration)}
	binding.analysis.SelfContained = profile.SelfContained
	binding.analysis.Requirements.RTP = profile.RTPDependencies
	state, _ := resolveRPGDependencies(binding)
	record.group.ValidationStatus, record.group.CompatibilityCode = state.status, state.code
	record.group.DependencySnapshot = state.snapshotJSON
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
