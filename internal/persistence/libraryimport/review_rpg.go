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
	var result application.RPGReviewProfile
	err := records.executor.QueryRowContext(ctx, `
SELECT binding.core_id,profile.generation,profile.evidence_generation,profile.evidence_confidence,
profile.self_contained_override,profile.dependency_snapshot_sha256,profile.analysis_json
FROM rpgmaker_review_profiles profile JOIN runtime_target_bindings binding
ON binding.provider_id=profile.provider_id AND binding.target_id=profile.target_id
WHERE profile.review_draft_id=?`,
		draftID).Scan(&result.SelectedCoreID,
		&result.Generation,
		&result.EvidenceGeneration,
		&result.EvidenceConfidence,
		&result.SelfContainedOverride,
		&result.DependencySHA256,
		&result.AnalysisJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return application.RPGReviewProfile{}, false, nil
	}
	if err != nil {
		return application.RPGReviewProfile{}, false, fmt.Errorf("read RPG review profile: %w", err)
	}
	return result, true, nil
}
