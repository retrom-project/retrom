package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	application "retrom/internal/service/libraryimport"
)

type rpgReviewBinding struct {
	generation       string
	override         bool
	dependencySHA256 string
	analysis         application.RPGReviewAnalysis
}

func (run *draftPatchRun) applyRPGMakerBinding() error {
	if !run.isRPG {
		if run.patch.RPGSelfContainedOverride != nil {
			return application.ErrInvalid
		}
		return nil
	}
	if run.targetOrDOSChanged {
		return application.ErrInvalid
	}
	if run.patch.RPGSelfContainedOverride == nil {
		return nil
	}
	profile, err := loadRPGReviewBinding(run.ctx, run.transaction, run.draftID)
	if err != nil {
		return err
	}
	override := *run.patch.RPGSelfContainedOverride
	if override && (profile.generation == "RPGMV" || profile.generation == "RPGMZ") {
		return application.ErrInvalid
	}
	_, err = run.transaction.ExecContext(run.ctx, `
UPDATE import_items SET review_profile_json=json_set(review_profile_json,'$.data.selfContainedOverride',?)
WHERE id=? AND review_profile_json IS NOT NULL
`, boolIncrement(override), run.draftID)
	if err != nil {
		return fmt.Errorf("libraryimport/review self-contained confirmation: %w", err)
	}
	return nil
}

func loadRPGReviewBinding(
	ctx context.Context, transaction *sql.Tx, draftID string,
) (rpgReviewBinding, error) {
	profile, err := readRPGReviewProfile(ctx, transaction, draftID)
	if err != nil {
		return rpgReviewBinding{}, application.ErrInvalid
	}
	result := rpgReviewBinding{
		generation: profile.Generation, override: profile.SelfContainedOverride != 0,
		dependencySHA256: profile.DependencySnapshotSHA256,
	}
	if err := json.Unmarshal(profile.Analysis, &result.analysis); err != nil {
		return rpgReviewBinding{}, application.ErrInvalid
	}
	return result, nil
}
