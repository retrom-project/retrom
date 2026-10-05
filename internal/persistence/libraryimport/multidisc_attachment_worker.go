package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/jobinput"

	dbapi "retrom/internal/database"

	libraryservice "retrom/internal/service/libraryimport"
)

type MultiDiscAttachmentWorker struct {
	database dbapi.DB
}

func NewMultiDiscAttachmentWorker(database dbapi.DB) *MultiDiscAttachmentWorker {
	return &MultiDiscAttachmentWorker{database: database}
}

func (repository *MultiDiscAttachmentWorker) Claim(
	ctx context.Context,
	jobID, workerID string,
	now int64,
) (libraryservice.MultiDiscAttachmentWorkerClaim, error) {
	return runWorkerClaim(
		ctx, repository.database, jobID, workerID, now, "multi-disc attachment",
		claimMultiDiscAttachmentRecords, loadMultiDiscAttachmentClaim,
	)
}

func loadMultiDiscAttachmentClaim(
	ctx context.Context,
	tx dbapi.Tx,
	jobID, workerID string,
) (libraryservice.MultiDiscAttachmentWorkerClaim, error) {
	input, startedAtMS, deadlineAtMS, err := readMultiDiscAttachmentInput(ctx, tx, jobID)
	if err != nil {
		return libraryservice.MultiDiscAttachmentWorkerClaim{}, err
	}
	attachment, err := readMultiDiscAttachmentRecord(ctx, tx, jobID)
	if err != nil {
		return libraryservice.MultiDiscAttachmentWorkerClaim{}, err
	}
	if !attachment.matches(input) {
		return libraryservice.MultiDiscAttachmentWorkerClaim{}, libraryservice.ErrInvalid
	}
	return libraryservice.MultiDiscAttachmentWorkerClaim{
		JobID:                jobID,
		WorkerID:             workerID,
		Input:                input,
		ExecutionStartedAtMS: startedAtMS,
		DeadlineAtMS:         deadlineAtMS,
	}, nil
}

func readMultiDiscAttachmentInput(
	ctx context.Context,
	tx dbapi.Tx,
	jobID string,
) (libraryservice.MultiDiscAttachmentInput, int64, int64, error) {
	var inputJSON, scopeID string
	var startedAtMS, deadlineAtMS int64
	if err := dbapi.QueryRowContext(ctx, tx, `
SELECT input.input_json,job.scope_id,job.execution_started_at_ms,job.execution_deadline_at_ms
FROM job_input_snapshots input
JOIN jobs job ON job.id=input.job_id AND job.execution_no=input.execution_no
WHERE input.job_id=?
	`, jobID).Scan(&inputJSON, &scopeID, &startedAtMS, &deadlineAtMS); err != nil {
		return libraryservice.MultiDiscAttachmentInput{}, 0, 0, fmt.Errorf("read multi-disc attachment input: %w", err)
	}
	var input libraryservice.MultiDiscAttachmentInput
	envelope, err := jobinput.Decode(
		[]byte(inputJSON), "REVIEW_MULTI_DISC_VALIDATE", jobinput.Scope{Type: "IMPORT_ITEM", ID: scopeID},
	)
	if err != nil {
		return libraryservice.MultiDiscAttachmentInput{}, 0, 0, fmt.Errorf("decode attachment input: %w", err)
	}
	if err := json.Unmarshal(envelope.Inputs, &input); err != nil {
		return libraryservice.MultiDiscAttachmentInput{}, 0, 0, libraryservice.ErrInvalid
	}
	if !libraryservice.ValidMultiDiscAttachmentInput(input) {
		return libraryservice.MultiDiscAttachmentInput{}, 0, 0, libraryservice.ErrInvalid
	}
	return input, startedAtMS, deadlineAtMS, nil
}

type multiDiscAttachmentRecord struct {
	ID, ItemID, DraftID, UserID, BaseSnapshotID string
	UploadID, ExpectedDigest, State             string
}

func readMultiDiscAttachmentRecord(
	ctx context.Context,
	tx dbapi.Tx,
	jobID string,
) (multiDiscAttachmentRecord, error) {
	var record multiDiscAttachmentRecord
	if err := dbapi.QueryRowContext(ctx, tx, `
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

func (record multiDiscAttachmentRecord) matches(input libraryservice.MultiDiscAttachmentInput) bool {
	return record.State == "PENDING" && record.ID == input.AttachmentID &&
		record.ItemID == input.ImportItemID && record.DraftID == input.ReviewDraftID &&
		record.UserID == input.RequestedByUserID && record.BaseSnapshotID == input.BaseSourceSnapshotID &&
		record.UploadID == input.UploadSessionID && record.ExpectedDigest == input.ExpectedSetDigest
}

func claimMultiDiscAttachmentRecords(ctx context.Context, tx dbapi.Tx, jobID, workerID string, now int64) error {
	return claimAttachmentRecords(
		ctx, tx, jobID, workerID, now, "REVIEW_MULTI_DISC_VALIDATE",
		libraryservice.MultiDiscAttachmentDeadline.Milliseconds(),
	)
}

func (repository *MultiDiscAttachmentWorker) Heartbeat(ctx context.Context, jobID, workerID string, now int64) error {
	return HeartbeatAttachment(ctx, repository.database, jobID, workerID, now)
}

func (repository *MultiDiscAttachmentWorker) BaseFiles(
	ctx context.Context, snapshotID string,
) (libraryservice.MultiDiscAttachmentBaseFiles, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT file.role,file.logical_name,file.upload_file_id,file.file_record,((blob.value)::jsonb #>> '{sha256}'),
 (((blob.value)::jsonb #>> '{size_bytes}'))::bigint,file.sort_order
FROM import_item_source_snapshot_files file
JOIN LATERAL (SELECT file.file_record AS value) blob ON blob.value IS NOT NULL
WHERE file.source_snapshot_id=? AND file.role IN ('PLAYLIST_SOURCE','DISC')
ORDER BY file.role,file.sort_order
`, snapshotID)
	if err != nil {
		return libraryservice.MultiDiscAttachmentBaseFiles{}, fmt.Errorf("query multi-disc base files: %w", err)
	}
	defer func() { cleanup.Error("close multi-disc base files", rows.Close()) }()
	result := libraryservice.MultiDiscAttachmentBaseFiles{Files: make([]libraryservice.MultiDiscAttachmentFile, 0)}
	for rows.Next() {
		var file libraryservice.MultiDiscAttachmentFile
		var uploadFileID sql.NullString
		if err := rows.Scan(
			&file.Role, &file.LogicalName, &uploadFileID, &file.FileRecord,
			&file.BlobSHA, &file.BlobSize, &file.SortOrder,
		); err != nil {
			return libraryservice.MultiDiscAttachmentBaseFiles{}, fmt.Errorf("scan multi-disc base file: %w", err)
		}
		if uploadFileID.Valid {
			file.UploadFileID = uploadFileID.String
		}
		result.Files = append(result.Files, file)
	}
	if err := rows.Err(); err != nil {
		return libraryservice.MultiDiscAttachmentBaseFiles{}, fmt.Errorf("iterate multi-disc base files: %w", err)
	}
	entries, err := BindMultiDiscAdmission(repository.database).Entries(ctx, snapshotID)
	if err != nil {
		return libraryservice.MultiDiscAttachmentBaseFiles{}, fmt.Errorf("read multi-disc base entries: %w", err)
	}
	result.Entries = entries
	return result, nil
}

func (repository *MultiDiscAttachmentWorker) UploadFiles(
	ctx context.Context, sessionID string,
) (libraryservice.MultiDiscAttachmentUploadFiles, error) {
	var result libraryservice.MultiDiscAttachmentUploadFiles
	var consumed bool
	if err := dbapi.QueryRowContext(ctx, repository.database, `
SELECT state,source_type,EXISTS(
  SELECT 1 FROM upload_consumptions consumption
  WHERE consumption.upload_session_id=upload_sessions.id AND consumption.upload_file_id IS NULL
)
FROM upload_sessions WHERE id=?
`, sessionID).Scan(&result.State, &result.SourceType, &consumed); err != nil {
		return libraryservice.MultiDiscAttachmentUploadFiles{}, fmt.Errorf("read multi-disc upload session: %w", err)
	}
	result.Consumed = consumed
	rows, err := repository.database.QueryContext(ctx, `
SELECT file.relative_path,file.id,file.file_record,((blob.value)::jsonb #>> '{sha256}'),
(((blob.value)::jsonb #>> '{size_bytes}'))::bigint
FROM import_files file
JOIN LATERAL (SELECT file.file_record AS value) blob ON blob.value IS NOT NULL
WHERE file.upload_session_id=? AND file.released_at_ms IS NULL
ORDER BY file.relative_path,file.id
`, sessionID)
	if err != nil {
		return libraryservice.MultiDiscAttachmentUploadFiles{}, fmt.Errorf("query multi-disc upload files: %w", err)
	}
	defer func() { cleanup.Error("close multi-disc upload files", rows.Close()) }()
	result.Files = make([]libraryservice.MultiDiscAttachmentFile, 0)
	for rows.Next() {
		var file libraryservice.MultiDiscAttachmentFile
		file.Role = "DISC"
		if err := rows.Scan(&file.LogicalName, &file.UploadFileID, &file.FileRecord, &file.BlobSHA,
			&file.BlobSize); err != nil {
			return libraryservice.MultiDiscAttachmentUploadFiles{}, fmt.Errorf("scan multi-disc upload file: %w", err)
		}
		result.Files = append(result.Files, file)
	}
	if err := rows.Err(); err != nil {
		return libraryservice.MultiDiscAttachmentUploadFiles{}, fmt.Errorf("iterate multi-disc upload files: %w", err)
	}
	return result, nil
}
