package libraryimport

import (
	"context"

	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/libraryimport"
)

func (records multidiscAdmissionRecords) Job(ctx context.Context, value application.MultiDiscAttachmentWrite) error {
	result, err := records.executor.ExecContext(ctx, `INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,
payload_json,cancellable,state,attempt_count,max_attempts,available_at_ms,
created_at_ms,updated_at_ms)
VALUES(?,'IMPORT_ITEM',?,'REVIEW_MULTI_DISC_VALIDATE',?,1,?,1,'QUEUED',0,4,?,?,?)`,
		value.JobID, value.Input.ImportItemID, value.DedupeKey, value.InputJSON, value.Now, value.Now, value.Now)
	return attachmentAdmissionCount(
		result, err, application.MultiDiscAttachmentErrorUnavailable,
	)
}

func (records multidiscAdmissionRecords) Input(ctx context.Context, value application.MultiDiscAttachmentWrite) error {
	result, err := records.executor.ExecContext(ctx, `INSERT INTO job_input_snapshots
(job_id,execution_no,input_json,input_digest,created_at_ms) VALUES(?,1,?,?,?)`,
		value.JobID, value.InputJSON, value.InputDigest, value.Now)
	return attachmentAdmissionCount(result, err, application.MultiDiscAttachmentErrorUnavailable)
}

func (records multidiscAdmissionRecords) Event(ctx context.Context, value application.MultiDiscAttachmentWrite) error {
	result, err := records.executor.ExecContext(ctx, `INSERT INTO job_events(
	job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'QUEUED','{"schemaVersion":1,"state":"QUEUED"}',?)`,
		value.JobID, value.Input.ImportItemID, value.Now)
	return attachmentAdmissionCount(result, err, application.MultiDiscAttachmentErrorUnavailable)
}

func (records multidiscAdmissionRecords) Attachment(
	ctx context.Context, value application.MultiDiscAttachmentWrite,
) error {
	input := value.Input
	result, err := recordstore.CreateReviewMultidiscAttachments(
		ctx, records.executor, `INSERT INTO review_multidisc_attachments
(id,import_item_id,review_draft_id,requested_by_user_id,base_source_snapshot_id,upload_session_id,
expected_set_digest,state,diagnostics_json,job_id,version,created_at_ms,updated_at_ms)
SELECT ?,?,?,?,?,?,?,'QUEUED','{}',?,1,?,? WHERE NOT EXISTS(
SELECT 1 FROM review_multidisc_attachments active WHERE active.import_item_id=?
	AND active.state IN ('QUEUED','RUNNING','FAILED_RETRYABLE'))`, input.AttachmentID,
		input.ImportItemID, input.ReviewDraftID,
		input.RequestedByUserID, input.BaseSourceSnapshotID, input.UploadSessionID, input.ExpectedSetDigest, value.JobID,
		value.Now, value.Now, input.ImportItemID)
	return attachmentAdmissionCount(result, err, application.MultiDiscAttachmentErrorInProgress)
}

func (records multidiscAdmissionRecords) Draft(ctx context.Context, value application.MultiDiscAttachmentWrite) error {
	result, err := recordstore.UpdateReviewDrafts(ctx, records.executor, recordstore.Update{
		Set: `version=version+1,updated_at_ms=?`, Values: []any{value.Now},
		Scope: recordstore.Scope{Where: `id=? AND version=? AND import_item_id=? AND effective_source_snapshot_id=?
AND target_platform_instance_id=?`, Args: []any{
			value.Input.ReviewDraftID, value.RequestVersion, value.Input.ImportItemID,
			value.Input.BaseSourceSnapshotID, value.Input.PlatformInstanceID,
		}},
	})
	return attachmentAdmissionCount(result, err, application.MultiDiscAttachmentErrorVersion)
}
