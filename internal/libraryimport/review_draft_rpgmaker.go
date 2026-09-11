package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"retrom/internal/rpgmaker/detector"
)

type rpgReviewAnalysis struct {
	SelfContained bool `json:"selfContained"`
	Requirements  struct {
		RTP []detector.RTPDependency `json:"rtpDependencies"`
	} `json:"requirements"`
}

type rpgReviewBinding struct {
	generation       string
	override         bool
	dependencySHA256 string
	analysis         rpgReviewAnalysis
}

func (run *draftPatchRun) applyRPGMakerBinding() error {
	if !run.isRPG {
		if run.patch.RPGSelfContainedOverride != nil {
			return ErrInvalid
		}
		return nil
	}
	if run.targetOrDOSChanged {
		return ErrInvalid
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
		return ErrInvalid
	}
	_, err = run.transaction.ExecContext(run.ctx, `
UPDATE rpgmaker_review_profiles SET self_contained_override=?,updated_at_ms=? WHERE review_draft_id=?
`, boolIncrement(override), run.service.now().UnixMilli(), run.draftID)
	if err != nil {
		return fmt.Errorf("libraryimport/review self-contained confirmation: %w", err)
	}
	return nil
}

func loadRPGReviewBinding(ctx context.Context, transaction *sql.Tx, draftID string) (rpgReviewBinding, error) {
	var result rpgReviewBinding
	var analysisJSON string
	if err := transaction.QueryRowContext(ctx, `
SELECT generation,self_contained_override,dependency_snapshot_sha256,analysis_json
FROM rpgmaker_review_profiles WHERE review_draft_id=?
`, draftID).Scan(
		&result.generation, &result.override, &result.dependencySHA256, &analysisJSON,
	); err != nil || json.Unmarshal([]byte(analysisJSON), &result.analysis) != nil {
		return rpgReviewBinding{}, ErrInvalid
	}
	return result, nil
}
