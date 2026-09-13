package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/persistence/dbexec"
	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

// The validation compatibility facade still reads the RPG profile while its
// broader refresh workflow is migrated. It accepts the shared executor so
// callers do not need to expose a concrete transaction type.
type rpgReviewAnalysis = application.RPGReviewAnalysis

type rpgReviewBinding struct {
	generation       string
	override         bool
	dependencySHA256 string
	analysis         rpgReviewAnalysis
}

func loadRPGReviewBinding(
	ctx context.Context, transaction dbexec.Executor, draftID string,
) (rpgReviewBinding, error) {
	profile, err := repository.BindReviewValidation(transaction).RPGProfile(ctx, draftID)
	if err != nil {
		return rpgReviewBinding{}, fmt.Errorf("read RPG review profile: %w", err)
	}
	analysis, err := profileAnalysis(profile)
	if err != nil {
		return rpgReviewBinding{}, err
	}
	return rpgReviewBinding{
		generation: profile.Generation, override: profile.SelfContainedOverride,
		dependencySHA256: profile.DependencySHA256,
		analysis:         analysis,
	}, nil
}

func profileAnalysis(profile application.RPGReviewProfile) (rpgReviewAnalysis, error) {
	var analysis rpgReviewAnalysis
	if err := json.Unmarshal([]byte(profile.AnalysisJSON), &analysis); err != nil {
		// RPGProfile validates the same payload before returning. Keep this
		// defensive branch total for callers using a test double.
		return rpgReviewAnalysis{}, fmt.Errorf("decode RPG review profile: %w", ErrInvalid)
	}
	return analysis, nil
}
