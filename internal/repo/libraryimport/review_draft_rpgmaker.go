package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	application "retrom/internal/model/libraryimport"
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
UPDATE rpgmaker_review_profiles SET self_contained_override=?,updated_at_ms=? WHERE review_draft_id=?
`, boolIncrement(override), run.repository.now().UnixMilli(), run.draftID)
	if err != nil {
		return fmt.Errorf("libraryimport/review self-contained confirmation: %w", err)
	}
	return nil
}

func loadRPGReviewBinding(
	ctx context.Context, transaction *sql.Tx, draftID string,
) (rpgReviewBinding, error) {
	var result rpgReviewBinding
	var analysisJSON string
	if err := transaction.QueryRowContext(ctx, `
SELECT generation,self_contained_override,dependency_snapshot_sha256,analysis_json
FROM rpgmaker_review_profiles WHERE review_draft_id=?
`, draftID).Scan(
		&result.generation, &result.override, &result.dependencySHA256, &analysisJSON,
	); err != nil {
		return rpgReviewBinding{}, application.ErrInvalid
	}
	if err := json.Unmarshal([]byte(analysisJSON), &result.analysis); err != nil {
		return rpgReviewBinding{}, application.ErrInvalid
	}
	return result, nil
}
