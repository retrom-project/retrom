package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

func (records *ReviewInputs) Profile(
	ctx context.Context,
	draftID string,
) (libraryservice.RPGReviewProfile, bool, error) {
	profile, err := readRPGReviewProfile(ctx, records.executor, draftID)
	if errors.Is(err, sql.ErrNoRows) {
		return libraryservice.RPGReviewProfile{}, false, nil
	}
	if err != nil {
		return libraryservice.RPGReviewProfile{}, false, fmt.Errorf("read RPG review profile: %w", err)
	}
	result := libraryservice.RPGReviewProfile{
		Generation: profile.Generation, EvidenceGeneration: profile.EvidenceGeneration,
		EvidenceConfidence:    profile.EvidenceConfidence,
		SelfContainedOverride: profile.SelfContainedOverride != 0,
		DependencySHA256:      profile.DependencySnapshotSHA256, AnalysisJSON: string(profile.Analysis),
	}
	err = dbapi.QueryRowContext(ctx, records.executor, `
SELECT core_id FROM runtime_target_bindings WHERE provider_id=? AND target_id=?`,
		profile.ProviderID, profile.TargetID).Scan(&result.SelectedCoreID)
	if err != nil {
		return libraryservice.RPGReviewProfile{}, false, fmt.Errorf("read RPG review target binding: %w", err)
	}
	return result, true, nil
}
