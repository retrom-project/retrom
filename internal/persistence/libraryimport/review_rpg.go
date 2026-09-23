package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	application "retrom/internal/service/libraryimport"
)

func (records *ReviewValidation) Profile(
	ctx context.Context,
	draftID string,
) (application.RPGReviewProfile, bool, error) {
	profile, err := readRPGReviewProfile(ctx, records.executor, draftID)
	if errors.Is(err, sql.ErrNoRows) {
		return application.RPGReviewProfile{}, false, nil
	}
	if err != nil {
		return application.RPGReviewProfile{}, false, fmt.Errorf("read RPG review profile: %w", err)
	}
	result := application.RPGReviewProfile{
		Generation: profile.Generation, EvidenceGeneration: profile.EvidenceGeneration,
		EvidenceConfidence:    profile.EvidenceConfidence,
		SelfContainedOverride: profile.SelfContainedOverride != 0,
		DependencySHA256:      profile.DependencySnapshotSHA256, AnalysisJSON: string(profile.Analysis),
	}
	err = records.executor.QueryRowContext(ctx, `
SELECT core_id FROM runtime_target_bindings WHERE provider_id=? AND target_id=?`,
		profile.ProviderID, profile.TargetID).Scan(&result.SelectedCoreID)
	if err != nil {
		return application.RPGReviewProfile{}, false, fmt.Errorf("read RPG review target binding: %w", err)
	}
	return result, true, nil
}
