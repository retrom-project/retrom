package libraryimport

import (
	"encoding/json"
	"fmt"
)

type approvalEvidence struct {
	before   []byte
	after    []byte
	diff     []byte
	config   []byte
	dat      []byte
	provider []byte
}

func (run *approvalRun) persistDecision() error {
	evidence := run.marshalApprovalEvidence()
	actor := reviewActor(run.ctx)
	_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO review_events(
  id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,before_json,
  after_json,diff_json,config_evidence_json,dat_evidence_json,provider_evidence_json,
  reason,created_at_ms
) VALUES(?,?,'APPROVED',?,?,?,?,?,?,?,?,?,?,?)
`, run.eventID, run.itemID, actor.Kind, actor.UserID, actor.Label, string(evidence.before),
		string(evidence.after), string(evidence.diff), string(evidence.config), string(evidence.dat),
		string(evidence.provider), run.input.decisionReason, run.now)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	if err := run.markItemPublished(); err != nil {
		return err
	}
	return transitionServerReview(
		run.ctx, run.transaction, run.itemID, "PUBLISHED", run.gameID, run.now,
	)
}

func (run *approvalRun) markItemPublished() error {
	_, err := run.transaction.ExecContext(run.ctx, `
UPDATE import_items
SET state='PUBLISHED',version=version+1,updated_at_ms=?,completed_at_ms=?
WHERE id=?
`, run.now, run.now, run.itemID)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	_, err = run.transaction.ExecContext(run.ctx, `
UPDATE import_jobs
SET review_pending_item_count=review_pending_item_count-1,
  published_item_count=published_item_count+1,
  state=CASE
    WHEN review_pending_item_count=1 AND rejected_file_count=resolved_rejected_file_count
      THEN 'COMPLETED'
    WHEN review_pending_item_count=1 THEN 'PARTIAL_FAILURE'
    ELSE state
  END,
  version=version+1,updated_at_ms=?,
  completed_at_ms=CASE
    WHEN review_pending_item_count=1 AND rejected_file_count=resolved_rejected_file_count
      THEN ? ELSE NULL
  END
WHERE id=?
`, run.now, run.now, run.importID)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	return nil
}

func (run *approvalRun) marshalApprovalEvidence() approvalEvidence {
	before, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "effectiveSourceSnapshotId": run.sourceSnapshotID,
		"metadata": json.RawMessage(run.metadataJSON), "selectedValidationId": run.validationID,
		"selectedCandidateId": nullable(run.candidateID),
		"selectedAssets": map[string]any{
			"coverCandidateAssetId":       nullable(run.coverID),
			"coverUploadedAssetId":        nullable(run.uploadedCoverID),
			"backgroundCandidateAssetId":  nullable(run.backgroundID),
			"screenshotCandidateAssetIds": run.screenshotIDs,
		},
		"defaultDosEntry": nullable(run.draftDOSEntry), "tags": run.publishedTags,
	})
	after, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "gameId": run.gameID, "metadataRevisionId": run.metadataID,
		"contentRevisionId": run.contentID, "variantRevisionId": run.variantRevisionID,
		"tags": run.publishedTags,
	})
	diff, _ := json.Marshal(run.approvalDiff())
	config, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "configSnapshot": json.RawMessage(run.configJSON),
		"validationId": run.validationID, "runtimeScreenshotId": nullable(run.approvalScreenshotID),
	})
	datEvidence, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "datVersionId": nullable(run.datID),
		"dependencySnapshot": json.RawMessage(run.dependencySnapshotJSON),
	})
	provider, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "selectedCandidateId": nullable(run.candidateID),
		"coverCandidateAssetId":       nullable(run.coverID),
		"coverUploadedAssetId":        nullable(run.uploadedCoverID),
		"backgroundCandidateAssetId":  nullable(run.backgroundID),
		"screenshotCandidateAssetIds": run.screenshotIDs,
	})
	return approvalEvidence{
		before: before, after: after, diff: diff, config: config, dat: datEvidence, provider: provider,
	}
}

func (run *approvalRun) approvalDiff() map[string]any {
	diff := map[string]any{
		"schemaVersion": 1, "decision": "APPROVED",
		"contentIdentityDigest": run.contentIdentityDigest, "tags": run.publishedTags,
	}
	if run.options.bulkApprovalID != "" {
		diff["approvalMode"] = "QUICK_STRICT_READY"
		diff["bulkApprovalId"] = run.options.bulkApprovalID
	}
	if run.screenshotOverride {
		diff["runtimeScreenshotOverride"] = map[string]any{
			"screenshotId": run.approvalScreenshotID.String, "validationId": run.validationID,
		}
	}
	if run.input.decision.DuplicatePolicy == "ALLOW_NEW" {
		diff["duplicatePolicy"] = run.input.decision.DuplicatePolicy
		diff["acknowledgedGameIds"] = duplicateIDs(run.duplicateGames)
	}
	return diff
}
