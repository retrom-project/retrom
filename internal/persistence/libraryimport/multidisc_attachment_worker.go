package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/libraryimport"
)

type MultiDiscAttachmentWorker struct {
	database *sql.DB
}

func NewMultiDiscAttachmentWorker(database *sql.DB) *MultiDiscAttachmentWorker {
	return &MultiDiscAttachmentWorker{database: database}
}

func (repository *MultiDiscAttachmentWorker) Claim(
	ctx context.Context,
	jobID, workerID string,
	now int64,
) (application.MultiDiscAttachmentWorkerClaim, error) {
	return runWorkerClaim(
		ctx, repository.database, jobID, workerID, now, "multi-disc attachment",
		claimMultiDiscAttachmentRecords, loadMultiDiscAttachmentClaim,
	)
}

func loadMultiDiscAttachmentClaim(
	ctx context.Context,
	tx *sql.Tx,
	jobID, workerID string,
) (application.MultiDiscAttachmentWorkerClaim, error) {
	input, startedAtMS, err := readMultiDiscAttachmentInput(ctx, tx, jobID)
	if err != nil {
		return application.MultiDiscAttachmentWorkerClaim{}, err
	}
	attachment, err := readMultiDiscAttachmentRecord(ctx, tx, jobID)
	if err != nil {
		return application.MultiDiscAttachmentWorkerClaim{}, err
	}
	if !attachment.matches(input) {
		return application.MultiDiscAttachmentWorkerClaim{}, application.ErrInvalid
	}
	return application.MultiDiscAttachmentWorkerClaim{
		JobID:                jobID,
		WorkerID:             workerID,
		Input:                input,
		ExecutionStartedAtMS: startedAtMS,
	}, nil
}

func readMultiDiscAttachmentInput(
	ctx context.Context,
	tx *sql.Tx,
	jobID string,
) (application.MultiDiscAttachmentInput, int64, error) {
	var inputJSON string
	var startedAtMS int64
	if err := tx.QueryRowContext(ctx, `
SELECT input.input_json,job.execution_started_at_ms
FROM job_input_snapshots input
JOIN jobs job ON job.id=input.job_id AND job.execution_no=input.execution_no
WHERE input.job_id=?
	`, jobID).Scan(&inputJSON, &startedAtMS); err != nil {
		return application.MultiDiscAttachmentInput{}, 0, fmt.Errorf("read multi-disc attachment input: %w", err)
	}
	var input application.MultiDiscAttachmentInput
	if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
		return application.MultiDiscAttachmentInput{}, 0, application.ErrInvalid
	}
	if !application.ValidMultiDiscAttachmentInput(input) {
		return application.MultiDiscAttachmentInput{}, 0, application.ErrInvalid
	}
	return input, startedAtMS, nil
}

type multiDiscAttachmentRecord struct {
	ID, ItemID, DraftID, UserID, BaseSnapshotID string
	UploadID, ExpectedDigest, State             string
}

func readMultiDiscAttachmentRecord(
	ctx context.Context,
	tx *sql.Tx,
	jobID string,
) (multiDiscAttachmentRecord, error) {
	var record multiDiscAttachmentRecord
	if err := tx.QueryRowContext(ctx, `
SELECT id,import_item_id,review_draft_id,requested_by_user_id,base_source_snapshot_id,
upload_session_id,expected_set_digest,state
FROM review_multidisc_attachments WHERE job_id=?
`, jobID).Scan(
		&record.ID, &record.ItemID, &record.DraftID, &record.UserID, &record.BaseSnapshotID,
		&record.UploadID, &record.ExpectedDigest, &record.State,
	); err != nil {
		return multiDiscAttachmentRecord{}, fmt.Errorf("read claimed multi-disc attachment: %w", err)
	}
	return record, nil
}

func (record multiDiscAttachmentRecord) matches(input application.MultiDiscAttachmentInput) bool {
	return record.State == "RUNNING" && record.ID == input.AttachmentID &&
		record.ItemID == input.ImportItemID && record.DraftID == input.ReviewDraftID &&
		record.UserID == input.RequestedByUserID && record.BaseSnapshotID == input.BaseSourceSnapshotID &&
		record.UploadID == input.UploadSessionID && record.ExpectedDigest == input.ExpectedSetDigest
}

func claimMultiDiscAttachmentRecords(ctx context.Context, tx *sql.Tx, jobID, workerID string, now int64) error {
	result, err := tx.ExecContext(ctx, `
UPDATE jobs SET state='RUNNING',attempt_count=attempt_count+1,worker_id=?,
execution_started_at_ms=COALESCE(execution_started_at_ms,?),
execution_deadline_at_ms=COALESCE(execution_deadline_at_ms,?),leased_until_ms=?,heartbeat_at_ms=?,
version=version+1,updated_at_ms=?
WHERE id=? AND kind='REVIEW_MULTI_DISC_VALIDATE' AND state='QUEUED' AND available_at_ms<=?
AND attempt_count<max_attempts
`, workerID, now, now+application.MultiDiscAttachmentDeadline.Milliseconds(),
		now+60_000, now, now, jobID, now)
	if err != nil {
		return fmt.Errorf("claim multi-disc attachment job: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ErrInvalid
	}
	result, err = recordstore.UpdateReviewMultidiscAttachments(ctx, tx, recordstore.Update{
		Set:    `state='RUNNING',error_code=NULL,finished_at_ms=NULL,version=version+1,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `job_id=? AND state IN ('QUEUED','FAILED_RETRYABLE')`, Args: []any{jobID}},
		Values: []any{now},
	})
	if err != nil {
		return fmt.Errorf("mark multi-disc attachment running: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ErrInvalid
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
	SELECT id,scope_type,scope_id,'STARTED','{"schemaVersion":1,"state":"RUNNING"}',? FROM jobs WHERE id=?
`, now, jobID); err != nil {
		return fmt.Errorf("record multi-disc attachment start: %w", err)
	}
	return nil
}

func (repository *MultiDiscAttachmentWorker) Heartbeat(ctx context.Context, jobID, workerID string, now int64) error {
	result, err := repository.database.ExecContext(ctx, `
UPDATE jobs SET leased_until_ms=?,heartbeat_at_ms=?,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=? AND execution_deadline_at_ms>?
`, now+60_000, now, now, jobID, workerID, now)
	if err != nil {
		return fmt.Errorf("heartbeat multi-disc attachment: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count multi-disc attachment heartbeat: %w", err)
	}
	if changed != 1 {
		return application.ErrInvalid
	}
	return nil
}

func (repository *MultiDiscAttachmentWorker) BaseFiles(
	ctx context.Context, snapshotID string,
) (application.MultiDiscAttachmentBaseFiles, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT file.role,file.logical_name,file.upload_file_id,file.blob_id,blob.sha256,blob.size_bytes,file.sort_order
FROM import_item_source_snapshot_files file
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.source_snapshot_id=? AND file.role IN ('PLAYLIST_SOURCE','DISC')
ORDER BY file.role,file.sort_order
`, snapshotID)
	if err != nil {
		return application.MultiDiscAttachmentBaseFiles{}, fmt.Errorf("query multi-disc base files: %w", err)
	}
	defer func() { cleanup.Error("close multi-disc base files", rows.Close()) }()
	result := application.MultiDiscAttachmentBaseFiles{Files: make([]application.MultiDiscAttachmentFile, 0)}
	for rows.Next() {
		var file application.MultiDiscAttachmentFile
		var uploadFileID sql.NullString
		if err := rows.Scan(
			&file.Role, &file.LogicalName, &uploadFileID, &file.BlobID,
			&file.BlobSHA, &file.BlobSize, &file.SortOrder,
		); err != nil {
			return application.MultiDiscAttachmentBaseFiles{}, fmt.Errorf("scan multi-disc base file: %w", err)
		}
		if uploadFileID.Valid {
			file.UploadFileID = uploadFileID.String
		}
		result.Files = append(result.Files, file)
	}
	if err := rows.Err(); err != nil {
		return application.MultiDiscAttachmentBaseFiles{}, fmt.Errorf("iterate multi-disc base files: %w", err)
	}
	entries, err := BindMultiDiscAdmission(repository.database).Entries(ctx, snapshotID)
	if err != nil {
		return application.MultiDiscAttachmentBaseFiles{}, fmt.Errorf("read multi-disc base entries: %w", err)
	}
	result.Entries = entries
	return result, nil
}

func (repository *MultiDiscAttachmentWorker) UploadFiles(
	ctx context.Context, sessionID string,
) (application.MultiDiscAttachmentUploadFiles, error) {
	var result application.MultiDiscAttachmentUploadFiles
	var consumed int
	if err := repository.database.QueryRowContext(ctx, `
SELECT state,source_type,EXISTS(
  SELECT 1 FROM upload_consumptions consumption
  WHERE consumption.upload_session_id=upload_sessions.id AND consumption.upload_file_id IS NULL
)
FROM upload_sessions WHERE id=?
`, sessionID).Scan(&result.State, &result.SourceType, &consumed); err != nil {
		return application.MultiDiscAttachmentUploadFiles{}, fmt.Errorf("read multi-disc upload session: %w", err)
	}
	result.Consumed = consumed != 0
	rows, err := repository.database.QueryContext(ctx, `
SELECT file.relative_path,file.id,file.blob_id,blob.sha256,blob.size_bytes
FROM import_files file
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.upload_session_id=? AND file.released_at_ms IS NULL
ORDER BY file.relative_path,file.id
`, sessionID)
	if err != nil {
		return application.MultiDiscAttachmentUploadFiles{}, fmt.Errorf("query multi-disc upload files: %w", err)
	}
	defer func() { cleanup.Error("close multi-disc upload files", rows.Close()) }()
	result.Files = make([]application.MultiDiscAttachmentFile, 0)
	for rows.Next() {
		var file application.MultiDiscAttachmentFile
		file.Role = "DISC"
		if err := rows.Scan(&file.LogicalName, &file.UploadFileID, &file.BlobID, &file.BlobSHA, &file.BlobSize); err != nil {
			return application.MultiDiscAttachmentUploadFiles{}, fmt.Errorf("scan multi-disc upload file: %w", err)
		}
		result.Files = append(result.Files, file)
	}
	if err := rows.Err(); err != nil {
		return application.MultiDiscAttachmentUploadFiles{}, fmt.Errorf("iterate multi-disc upload files: %w", err)
	}
	return result, nil
}
