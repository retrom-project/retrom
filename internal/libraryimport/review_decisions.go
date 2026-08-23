package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/payloadrelease"
	"retrom/internal/tagging"

	"github.com/google/uuid"
)

type DecisionResult struct {
	ItemID      string `json:"itemId"`
	EventID     string `json:"reviewEventId"`
	Status      string `json:"status"`
	Version     int64  `json:"version"`
	UpdatedAtMS int64  `json:"updatedAtMs"`
}

func requireSingleReviewMutation(result sql.Result, err error, action string) error {
	if err != nil {
		return fmt.Errorf("libraryimport/review: %s: %w", action, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("libraryimport/review: %s result: %w", action, err)
	}
	if changed != 1 {
		return ErrInvalid
	}
	return nil
}

func discardReviewItemAndAggregate(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	importID string,
	now int64,
) error {
	itemResult, itemErr := transaction.ExecContext(ctx, `
UPDATE import_items
SET state='DISCARDED',
version=version+1,
updated_at_ms=?,
completed_at_ms=?
WHERE id=?
AND state='REVIEW_PENDING'
`, now, now, itemID)
	if err := requireSingleReviewMutation(itemResult, itemErr, "discard item"); err != nil {
		return err
	}
	jobResult, jobErr := transaction.ExecContext(ctx, `
UPDATE import_jobs
SET review_pending_item_count=review_pending_item_count-1,
discarded_item_count=discarded_item_count+1,
state=CASE WHEN review_pending_item_count=1
AND rejected_file_count=resolved_rejected_file_count THEN 'COMPLETED'
WHEN review_pending_item_count=1 THEN 'PARTIAL_FAILURE'
ELSE state END,
version=version+1,
updated_at_ms=?,
completed_at_ms=CASE WHEN review_pending_item_count=1
AND rejected_file_count=resolved_rejected_file_count THEN ? ELSE NULL END
WHERE id=? AND review_pending_item_count>0
`, now, now, importID)
	return requireSingleReviewMutation(jobResult, jobErr, "discard job aggregate")
}

// Contract branches stay contiguous for a single auditable decision.
func (service *Service) Discard(
	ctx context.Context,
	itemID string,
	expectedVersion int64,
	reason string,
) (DecisionResult, error) {
	reason = strings.TrimSpace(reason)
	if reason != "" && !validField(reason, 500, true) {
		return DecisionResult{}, ErrInvalid
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return DecisionResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	defer cleanup.Rollback(transaction)
	evidence, err := service.loadDiscardEvidence(ctx, transaction, itemID, expectedVersion)
	if err != nil {
		return DecisionResult{}, err
	}
	now := service.now().UnixMilli()
	if err := cancelDiscardedReviewAttachments(ctx, transaction, itemID, now); err != nil {
		return DecisionResult{}, err
	}
	if err := discardReviewItemAndAggregate(ctx, transaction, itemID, evidence.importID, now); err != nil {
		return DecisionResult{}, err
	}
	eventID, err := insertDiscardReviewEvent(ctx, transaction, itemID, reason, evidence, now)
	if err != nil {
		return DecisionResult{}, err
	}
	if err := transitionServerReview(ctx, transaction, itemID, "REVIEW_DISCARDED", nil, now); err != nil {
		return DecisionResult{}, err
	}
	if err := scheduleTerminalPayloads(
		ctx, transaction, itemID, evidence.importID, payloadrelease.ReasonImportDiscarded, now,
	); err != nil {
		return DecisionResult{}, err
	}
	if err := transaction.Commit(); err != nil {
		return DecisionResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	return DecisionResult{
		ItemID: itemID, EventID: eventID, Status: "DISCARDED",
		Version: evidence.currentVersion + 1, UpdatedAtMS: now,
	}, nil
}

type discardEvidence struct {
	draftID            string
	importID           string
	metadataJSON       string
	configSnapshotJSON string
	validationID       sql.NullString
	datID              sql.NullString
	dependencySnapshot sql.NullString
	candidateID        sql.NullString
	coverID            sql.NullString
	uploadedCoverID    sql.NullString
	backgroundID       sql.NullString
	currentVersion     int64
	tags               []tagging.Reference
}

func (service *Service) loadDiscardEvidence(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	expectedVersion int64,
) (discardEvidence, error) {
	var value discardEvidence
	err := transaction.QueryRowContext(ctx, `
SELECT d.id,i.import_job_id,d.metadata_json,d.version,
  j.config_snapshot_json,d.selected_validation_id,v.dat_version_id,v.dependency_snapshot_json,
  d.selected_candidate_id,d.cover_candidate_asset_id,d.cover_uploaded_asset_id,
  d.background_candidate_asset_id
FROM import_items i
JOIN import_jobs j ON j.id=i.import_job_id
JOIN review_drafts d ON d.import_item_id=i.id
LEFT JOIN import_item_core_validations v ON v.id=d.selected_validation_id
WHERE i.id=? AND i.state='REVIEW_PENDING'
AND (i.review_handoff_kind='DIRECT' OR EXISTS(
  SELECT 1 FROM emulationstation_import_items reserved_source
  WHERE reserved_source.library_import_item_id=i.id
  AND reserved_source.execution_state='REVIEW_PENDING'
))
`, itemID).Scan(
		&value.draftID, &value.importID, &value.metadataJSON, &value.currentVersion,
		&value.configSnapshotJSON, &value.validationID,
		&value.datID, &value.dependencySnapshot, &value.candidateID, &value.coverID,
		&value.uploadedCoverID, &value.backgroundID,
	)
	if err != nil || value.currentVersion != expectedVersion {
		return discardEvidence{}, ErrInvalid
	}
	value.tags, err = service.tags.ReviewDraftReferences(ctx, transaction, value.draftID)
	if err != nil {
		return discardEvidence{}, fmt.Errorf("libraryimport/review: read discarded draft tags: %w", err)
	}
	return value, nil
}

func cancelDiscardedReviewAttachments(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	now int64,
) error {
	statements := []struct {
		query     string
		arguments []any
	}{
		{`UPDATE jobs
SET state=CASE WHEN state='QUEUED' THEN 'CANCELLED' ELSE 'CANCEL_REQUESTED' END,
  cancel_requested_at_ms=?,cancel_reason='review discarded',
  finished_at_ms=CASE WHEN state='QUEUED' THEN ? ELSE NULL END,
  version=version+1,updated_at_ms=?
WHERE id IN (SELECT job_id FROM review_arcade_parent_attachments
  WHERE import_item_id=? AND state IN ('QUEUED','RUNNING'))
  AND state IN ('QUEUED','RUNNING')`, []any{now, now, now, itemID}},
		{`UPDATE review_arcade_parent_attachments
SET state='CANCELLED',error_code='CANCELLED',
  diagnostics_json='{"errorCode":"CANCELLED","schemaVersion":1}',finished_at_ms=?,
  version=version+1,updated_at_ms=?
WHERE import_item_id=? AND state IN ('QUEUED','RUNNING')`, []any{now, now, itemID}},
		{`UPDATE jobs
SET state=CASE WHEN state='RUNNING' THEN 'CANCEL_REQUESTED' ELSE 'CANCELLED' END,
  cancel_requested_at_ms=?,cancel_reason='review discarded',
  finished_at_ms=CASE WHEN state='RUNNING' THEN NULL ELSE ? END,
  version=version+1,updated_at_ms=?
WHERE id IN (SELECT job_id FROM review_multidisc_attachments
  WHERE import_item_id=? AND state IN ('QUEUED','RUNNING','FAILED_RETRYABLE'))
  AND (state IN ('QUEUED','RUNNING') OR state='FAILED' AND error_retryable=1)`, []any{now, now, now, itemID}},
		{`UPDATE review_multidisc_attachments
SET state='CANCELLED',error_code='CANCELLED',
  diagnostics_json='{"errorCode":"CANCELLED","schemaVersion":1}',finished_at_ms=?,
  version=version+1,updated_at_ms=?
WHERE import_item_id=? AND state IN ('QUEUED','FAILED_RETRYABLE')`, []any{now, now, itemID}},
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.query, statement.arguments...); err != nil {
			return fmt.Errorf("libraryimport/review: %w", err)
		}
	}
	return nil
}

func insertDiscardReviewEvent(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	reason string,
	evidence discardEvidence,
	now int64,
) (string, error) {
	beforeJSON, configJSON, datJSON, providerJSON := marshalDiscardEvidence(evidence)
	eventID, _ := uuid.NewV7()
	actor := reviewActor(ctx)
	_, err := transaction.ExecContext(ctx, `
INSERT INTO review_events(
  id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,before_json,
  after_json,diff_json,config_evidence_json,dat_evidence_json,provider_evidence_json,
  reason,created_at_ms
) VALUES(?,?,'DISCARDED',?,?,?,?, 
  '{"schemaVersion":2,"decision":"DISCARDED"}',
  '{"schemaVersion":2,"decision":"DISCARDED"}',?,?,?,?,?)
`, eventID.String(), itemID, actor.Kind, actor.UserID, actor.Label, string(beforeJSON),
		string(configJSON), string(datJSON), string(providerJSON), nullableText(reason), now)
	if err != nil {
		return "", fmt.Errorf("libraryimport/review: %w", err)
	}
	return eventID.String(), nil
}

func marshalDiscardEvidence(evidence discardEvidence) ([]byte, []byte, []byte, []byte) {
	beforeJSON, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "metadata": json.RawMessage(evidence.metadataJSON),
		"tags": evidence.tags,
		"mediaSelection": map[string]any{
			"cover":      evidence.coverID.Valid || evidence.uploadedCoverID.Valid,
			"background": evidence.backgroundID.Valid,
		},
	})
	configJSON, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "validationAvailable": evidence.validationID.Valid,
	})
	datJSON, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "datMatched": evidence.datID.Valid,
	})
	providerJSON, _ := json.Marshal(map[string]any{
		"schemaVersion": 2, "selectedCandidateId": nullable(evidence.candidateID),
		"candidateSelected": evidence.candidateID.Valid,
	})
	return beforeJSON, configJSON, datJSON, providerJSON
}

type RetryResult struct {
	ItemID  string `json:"itemId"`
	JobID   string `json:"jobId"`
	State   string `json:"state"`
	Version int64  `json:"version"`
}

// Retry eligibility, execution creation, event emission, and aggregate update share one transaction.
func (service *Service) RetryItem(ctx context.Context, itemID string, expectedVersion int64) (RetryResult, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return RetryResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var importID, stage, manifestDigest string
	var version int64
	if err := transaction.QueryRowContext(ctx, `
SELECT import_job_id,
failed_stage,
source_manifest_digest,
version
FROM import_items
WHERE id=?
AND state='FAILED_RETRYABLE'
`, itemID).Scan(&importID, &stage, &manifestDigest, &version); err != nil ||
		version != expectedVersion {
		return RetryResult{}, ErrInvalid
	}
	jobID, _ := uuid.NewV7()
	now := service.now().UnixMilli()
	dedupe := sha256.Sum256([]byte(itemID + ":" + stage + ":" + time.UnixMilli(now).UTC().Format(time.RFC3339Nano)))
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(id,
scope_type,
scope_id,
kind,
dedupe_key,
execution_no,
payload_json,
cancellable,
state,
attempt_count,
max_attempts,
available_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
'IMPORT_ITEM',
?,
'IMPORT_ITEM_PIPELINE',
?,
1,
?,
1,
'QUEUED',
0,
2,
?,
?,
?)
`,
		jobID.String(),
		itemID,
		hex.EncodeToString(dedupe[:]),
		`{"sourceManifestDigest":"`+manifestDigest+`"}`,
		now,
		now,
		now,
	); err != nil {
		return RetryResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	itemResult, err := transaction.ExecContext(ctx, `
UPDATE import_items
SET state='QUEUED',
failed_stage=NULL,
last_error_code=NULL,
version=version+1,
updated_at_ms=?
WHERE id=? AND state='FAILED_RETRYABLE'
`, now, itemID)
	if err := requireSingleReviewMutation(itemResult, err, "retry item"); err != nil {
		return RetryResult{}, err
	}
	jobResult, err := transaction.ExecContext(ctx, `
UPDATE import_jobs
SET failed_item_count=failed_item_count-1,
queued_item_count=queued_item_count+1,
state='RUNNING',
version=version+1,
updated_at_ms=?
WHERE id=? AND failed_item_count>0
`, now, importID)
	if err := requireSingleReviewMutation(jobResult, err, "retry job aggregate"); err != nil {
		return RetryResult{}, err
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'IMPORT_ITEM',
?,
'MANUAL_RETRY',
'{}',
?)
`, jobID.String(), itemID, now); err != nil {
		return RetryResult{}, fmt.Errorf("libraryimport/review: retry event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return RetryResult{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	return RetryResult{ItemID: itemID, JobID: jobID.String(), State: "QUEUED", Version: version + 1}, nil
}

type CancelResult struct {
	ImportJobID string `json:"importJobId"`
	State       string `json:"state"`
	Version     int64  `json:"version"`
}

func (service *Service) Cancel(
	ctx context.Context,
	importID string,
	expectedVersion int64,
	reason string,
) (CancelResult, bool, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || !validField(reason, 500, true) {
		return CancelResult{}, false, ErrInvalid
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return CancelResult{}, false, fmt.Errorf("libraryimport/review: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state string
	var version, running, queued, reviewPending, retryableFailed int64
	if err := transaction.QueryRowContext(ctx, `
SELECT state,
version,
running_item_count,
(SELECT count(*) FROM import_items WHERE import_job_id=import_jobs.id AND state='QUEUED'),
(SELECT count(*) FROM import_items WHERE import_job_id=import_jobs.id AND state='REVIEW_PENDING'),
(SELECT count(*) FROM import_items WHERE import_job_id=import_jobs.id AND state='FAILED_RETRYABLE')
FROM import_jobs
WHERE id=?
`, importID).Scan(&state, &version, &running, &queued, &reviewPending, &retryableFailed); err != nil ||
		version != expectedVersion ||
		state == "COMPLETED" ||
		state == "CANCELLED" ||
		state == "FAILED" {
		return CancelResult{}, false, ErrInvalid
	}
	now := service.now().UnixMilli()
	pending := running > 0
	newState := "CANCELLED"
	if pending {
		newState = "CANCEL_REQUESTED"
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_items
SET state='CANCELLED',
failed_stage=NULL,
last_error_code=NULL,
completed_at_ms=?,
updated_at_ms=?,
version=version+1
WHERE import_job_id=?
AND state IN ('QUEUED',
'REVIEW_PENDING',
'FAILED_RETRYABLE')
`, now, now, importID); err != nil {
		return CancelResult{}, false, fmt.Errorf("libraryimport/review: cancel items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_jobs
SET state=?,
cancel_requested_at_ms=?,
cancel_reason=?,
cancelled_item_count=cancelled_item_count+?+?+?,
queued_item_count=queued_item_count-?,
review_pending_item_count=review_pending_item_count-?,
failed_item_count=failed_item_count-?,
version=version+1,
updated_at_ms=?,
completed_at_ms=CASE WHEN ?='CANCELLED' THEN ? ELSE NULL END
WHERE id=?
`, newState, now, reason,
		queued, reviewPending, retryableFailed,
		queued, reviewPending, retryableFailed,
		now, newState, now, importID); err != nil {
		return CancelResult{}, false, fmt.Errorf("libraryimport/review: cancel job aggregate: %w", err)
	}
	if err := scheduleCancelledPayloads(ctx, transaction, importID, now); err != nil {
		return CancelResult{}, false, err
	}
	if err := transaction.Commit(); err != nil {
		return CancelResult{}, false, fmt.Errorf("libraryimport/review: %w", err)
	}
	return CancelResult{ImportJobID: importID, State: newState, Version: version + 1}, pending, nil
}

func scheduleCancelledPayloads(ctx context.Context, transaction *sql.Tx, importID string, now int64) error {
	itemIDs, err := payloadrelease.CollectScopeIDs(ctx, transaction, `
SELECT id FROM import_items
WHERE import_job_id=? AND state='CANCELLED' AND payload_state='RETAINED'
ORDER BY id
`, importID)
	if err != nil {
		return fmt.Errorf("libraryimport/review: list cancelled payloads: %w", err)
	}
	for _, itemID := range itemIDs {
		if _, err := payloadrelease.ScheduleTerminalImportItem(
			ctx, transaction, itemID, payloadrelease.ReasonImportCancelled, now,
		); err != nil {
			return fmt.Errorf("libraryimport/review: schedule cancelled payload: %w", err)
		}
	}
	if _, err := payloadrelease.ScheduleTerminalImportJob(ctx, transaction, importID, now); err != nil {
		return fmt.Errorf("libraryimport/review: schedule cancelled aggregate: %w", err)
	}
	return nil
}
