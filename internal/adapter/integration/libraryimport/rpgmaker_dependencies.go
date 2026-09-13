package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/persistence/dbexec"
	repository "retrom/internal/persistence/libraryimport"
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

func loadReviewRPGDependencies(ctx context.Context, transaction dbexec.Executor, draftID string) (
	rpgReviewBinding, string, draftDependencyState, error,
) {
	profile, err := loadRPGReviewBinding(ctx, transaction, draftID)
	if err != nil {
		return profile, "", draftDependencyState{}, err
	}
	state, digest := resolveRPGDependencies(profile)
	return profile, digest, state, nil
}

func (state *draftValidationRefresh) resolveRPGDependencies() (draftDependencyState, error) {
	_, digest, result, err := loadReviewRPGDependencies(state.ctx, state.transaction, state.draftID)
	if err != nil {
		return draftDependencyState{}, err
	}
	if err := repository.BindReviewValidation(state.transaction).UpdateRPGDependencyDigest(
		state.ctx, state.draftID, digest, state.service.now().UnixMilli(),
	); err != nil {
		return draftDependencyState{}, fmt.Errorf("libraryimport/RPG dependency digest: %w", err)
	}
	return result, nil
}
