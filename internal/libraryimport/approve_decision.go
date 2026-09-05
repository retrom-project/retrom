package libraryimport

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"retrom/internal/payloadrelease"
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
	if err := transitionServerReview(
		run.ctx, run.transaction, run.itemID, "PUBLISHED", run.gameID, run.now,
	); err != nil {
		return err
	}
	return scheduleTerminalPayloads(
		run.ctx, run.transaction, run.itemID, run.importID, payloadrelease.ReasonImportPublished, run.now,
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
		"schemaVersion": 2, "metadata": json.RawMessage(run.metadataJSON),
		"mediaSelection": map[string]any{
			"cover":      hasText(run.coverID) || run.uploadedCoverID.Valid,
			"background": hasText(run.backgroundID), "screenshotCount": len(run.screenshotIDs),
		},
		"defaultDosEntry": nullable(run.draftDOSEntry), "tags": run.publishedTags,
	})
	after, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "gameId": run.gameID, "gameVariantId": run.variantID,
		"tags": run.publishedTags,
	})
	diff, _ := json.Marshal(run.approvalDiff())
	configEvidence := map[string]any{
		"schemaVersion": 2, "validation": "READY", "runtimeScreenshotOverride": run.screenshotOverride,
	}
	config, _ := json.Marshal(configEvidence)
	datEvidence, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "datMatched": run.datID.Valid,
	})
	provider, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "selectedCandidateId": nullable(run.candidateID),
		"candidateSelected": run.candidateID.Valid,
	})
	return approvalEvidence{
		before: before, after: after, diff: diff, config: config, dat: datEvidence, provider: provider,
	}
}

func (run *approvalRun) approvalDiff() map[string]any {
	mediaChanged := hasText(run.coverID) || run.uploadedCoverID.Valid ||
		hasText(run.backgroundID) || len(run.screenshotIDs) > 0
	diff := map[string]any{
		"schemaVersion": 2, "decision": "APPROVED", "tags": run.publishedTags,
		"mediaChanged": mediaChanged,
	}
	if run.options.bulkApprovalID != "" {
		diff["approvalMode"] = "QUICK_STRICT_READY"
		diff["bulkApprovalId"] = run.options.bulkApprovalID
	}
	if run.screenshotOverride {
		diff["runtimeScreenshotOverride"] = true
	}
	if run.input.decision.DuplicatePolicy == "ALLOW_NEW" {
		diff["duplicatePolicy"] = run.input.decision.DuplicatePolicy
		diff["acknowledgedGameIds"] = duplicateIDs(run.duplicateGames)
	}
	return diff
}

func hasText(value sql.NullString) bool { return value.Valid && value.String != "" }
