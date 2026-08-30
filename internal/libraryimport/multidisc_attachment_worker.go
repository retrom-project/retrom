package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"path"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/multidisc"
)

func (service *Service) ResumeMultiDiscAttachmentJobs(ctx context.Context) {
	now := service.now().UnixMilli()
	for _, job := range service.queuedJobRuns(ctx, "REVIEW_MULTI_DISC_VALIDATE") {
		service.scheduleMultiDiscAttachmentRun(ctx, job.id, time.Duration(job.availableAt-now)*time.Millisecond)
	}
}

func (service *Service) scheduleMultiDiscAttachmentRun(
	ctx context.Context,
	jobID string,
	delay time.Duration,
) {
	workerContext := context.WithoutCancel(ctx)
	if delay <= 0 {
		go service.runMultiDiscAttachment(workerContext, jobID)
		return
	}
	time.AfterFunc(delay, func() { service.runMultiDiscAttachment(workerContext, jobID) })
}

func (service *Service) claimMultiDiscAttachment(
	ctx context.Context,
	jobID string,
) (multiDiscAttachmentCandidate, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return multiDiscAttachmentCandidate{}, multiDiscAttachmentStoreError("begin claim", err)
	}
	defer cleanup.Rollback(transaction)
	workerID, _ := uuid.NewV7()
	now := service.now().UnixMilli()
	if err := claimMultiDiscAttachmentRecords(ctx, transaction, jobID, workerID.String(), now); err != nil {
		return multiDiscAttachmentCandidate{}, err
	}
	candidate, err := readClaimedMultiDiscAttachment(ctx, transaction, jobID)
	if err != nil {
		return multiDiscAttachmentCandidate{}, err
	}
	candidate.jobID, candidate.workerID = jobID, workerID.String()
	if err := transaction.Commit(); err != nil {
		return multiDiscAttachmentCandidate{}, multiDiscAttachmentStoreError("commit claim", err)
	}
	return candidate, nil
}

func claimMultiDiscAttachmentRecords(
	ctx context.Context,
	transaction *sql.Tx,
	jobID, workerID string,
	now int64,
) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='RUNNING',attempt_count=attempt_count+1,worker_id=?,
execution_started_at_ms=COALESCE(execution_started_at_ms,?),
execution_deadline_at_ms=COALESCE(execution_deadline_at_ms,?),leased_until_ms=?,heartbeat_at_ms=?,
version=version+1,updated_at_ms=?
WHERE id=? AND kind='REVIEW_MULTI_DISC_VALIDATE' AND state='QUEUED' AND available_at_ms<=?
AND attempt_count<max_attempts
`, workerID, now, now+int64(multiDiscAttachmentDeadline/time.Millisecond),
		now+60_000, now, now, jobID, now)
	if err != nil {
		return multiDiscAttachmentStoreError("claim job", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrInvalid
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE review_multidisc_attachments
SET state='RUNNING',error_code=NULL,finished_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE job_id=? AND state IN ('QUEUED','FAILED_RETRYABLE')
	`, now, jobID)
	if err != nil {
		return multiDiscAttachmentStoreError("mark attachment running", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrInvalid
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
	SELECT id,scope_type,scope_id,'STARTED','{"schemaVersion":1,"state":"RUNNING"}',? FROM jobs WHERE id=?
	`, now, jobID); err != nil {
		return multiDiscAttachmentStoreError("record start", err)
	}
	return nil
}

func readClaimedMultiDiscAttachment(
	ctx context.Context,
	transaction *sql.Tx,
	jobID string,
) (multiDiscAttachmentCandidate, error) {
	var candidate multiDiscAttachmentCandidate
	var inputJSON string
	if err := transaction.QueryRowContext(ctx, `
SELECT input.input_json,job.execution_started_at_ms
FROM job_input_snapshots input
JOIN jobs job ON job.id=input.job_id AND job.execution_no=input.execution_no
WHERE input.job_id=?
	`, jobID).Scan(&inputJSON, &candidate.executionStartedAtMS); err != nil {
		return multiDiscAttachmentCandidate{}, multiDiscAttachmentStoreError("read frozen input", err)
	}
	if err := json.Unmarshal([]byte(inputJSON), &candidate.input); err != nil ||
		!validMultiDiscAttachmentInput(candidate.input) {
		return multiDiscAttachmentCandidate{}, ErrInvalid
	}
	var attachmentID, itemID, draftID, userID, baseSnapshotID, uploadID, expectedDigest, state string
	if err := transaction.QueryRowContext(ctx, `
SELECT id,import_item_id,review_draft_id,requested_by_user_id,base_source_snapshot_id,
upload_session_id,expected_set_digest,state
FROM review_multidisc_attachments WHERE job_id=?
`, jobID).Scan(
		&attachmentID, &itemID, &draftID, &userID, &baseSnapshotID,
		&uploadID, &expectedDigest, &state,
	); err != nil {
		return multiDiscAttachmentCandidate{}, multiDiscAttachmentStoreError("read claimed attachment", err)
	}
	matches := state == "RUNNING" && attachmentID == candidate.input.AttachmentID &&
		itemID == candidate.input.ImportItemID && draftID == candidate.input.ReviewDraftID &&
		userID == candidate.input.RequestedByUserID && baseSnapshotID == candidate.input.BaseSourceSnapshotID &&
		uploadID == candidate.input.UploadSessionID && expectedDigest == candidate.input.ExpectedSetDigest
	if !matches {
		return multiDiscAttachmentCandidate{}, ErrInvalid
	}
	return candidate, nil
}

func multiDiscAttachmentDurationMS(candidate multiDiscAttachmentCandidate, now int64) int64 {
	if candidate.executionStartedAtMS <= 0 || now <= candidate.executionStartedAtMS {
		return 0
	}
	return now - candidate.executionStartedAtMS
}

func validMultiDiscAttachmentInput(input multiDiscAttachmentInput) bool {
	return input.SchemaVersion == 1 && input.AttachmentID != "" && input.ImportItemID != "" &&
		input.RequestedByUserID != "" && input.MaxDiscs >= multidisc.MinDiscs &&
		input.MaxDiscs <= multidisc.MaxDiscs && input.MaxTotalBytes >= 1 &&
		len(input.CompatibilityDigest) == sha256.Size*2
}

func (service *Service) readAttachedMultiDiscBase(
	ctx context.Context,
	candidate *multiDiscAttachmentCandidate,
) error {
	rows, err := service.database.QueryContext(ctx, `
SELECT file.role,file.logical_name,file.upload_file_id,file.blob_id,blob.sha256,blob.size_bytes,file.sort_order
FROM import_item_source_snapshot_files file
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.source_snapshot_id=? AND file.role IN ('PLAYLIST_SOURCE','DISC')
ORDER BY file.role,file.sort_order
`, candidate.input.BaseSourceSnapshotID)
	if err != nil {
		return multiDiscAttachmentStoreError("read base files", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var file attachedMultiDiscFile
		if err := rows.Scan(
			&file.role, &file.logicalName, &file.uploadFileID, &file.blobID,
			&file.blobSHA, &file.blobSize, &file.sortOrder,
		); err != nil {
			return multiDiscAttachmentStoreError("scan base files", err)
		}
		candidate.baseFiles = append(candidate.baseFiles, file)
	}
	if err := rows.Err(); err != nil {
		return multiDiscAttachmentStoreError("iterate base files", err)
	}
	entries, err := loadMultiDiscEntries(ctx, service.database, candidate.input.BaseSourceSnapshotID)
	if err != nil {
		return err
	}
	candidate.baseEntries = entries
	candidate.expectedMissing = missingMultiDiscEntries(entries)
	digest, err := multidisc.ExpectedSetDigest(entries)
	if err != nil || digest != candidate.input.ExpectedSetDigest || len(candidate.expectedMissing) == 0 {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, ErrInvalid)
	}
	return nil
}

func (service *Service) readMultiDiscAttachmentUploads(
	ctx context.Context,
	candidate *multiDiscAttachmentCandidate,
) error {
	var state, sourceType string
	var consumed int
	if err := service.database.QueryRowContext(ctx, `
SELECT state,source_type,EXISTS(
  SELECT 1 FROM upload_consumptions consumption
  WHERE consumption.upload_session_id=upload_sessions.id AND consumption.upload_file_id IS NULL
)
FROM upload_sessions WHERE id=?
`, candidate.input.UploadSessionID).Scan(&state, &sourceType, &consumed); err != nil ||
		state != "COMPLETE" || sourceType != "FILES" || consumed != 0 {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorInputStale, err)
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT file.relative_path,file.id,file.final_blob_id,blob.sha256,blob.size_bytes
FROM upload_files file
JOIN blobs blob ON blob.id=file.final_blob_id
WHERE file.upload_session_id=? AND file.state='COMPLETE'
ORDER BY file.relative_path,file.id
`, candidate.input.UploadSessionID)
	if err != nil {
		return multiDiscAttachmentStoreError("read uploads", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var file attachedMultiDiscFile
		if err := rows.Scan(
			&file.logicalName, &file.uploadFileID, &file.blobID, &file.blobSHA, &file.blobSize,
		); err != nil {
			return multiDiscAttachmentStoreError("scan uploads", err)
		}
		file.role = "DISC"
		candidate.uploadFiles = append(candidate.uploadFiles, file)
	}
	if err := rows.Err(); err != nil {
		return multiDiscAttachmentStoreError("iterate uploads", err)
	}
	return nil
}

func validateMultiDiscAttachmentSet(
	missing []multidisc.Entry,
	uploads []attachedMultiDiscFile,
) error {
	if len(missing) != len(uploads) {
		return multiDiscAttachmentError(MultiDiscAttachmentErrorSetMismatch, ErrInvalid)
	}
	expected := make(map[string]struct{}, len(missing))
	for _, entry := range missing {
		expected[entry.NormalizedReference] = struct{}{}
	}
	observed := make(map[string]struct{}, len(uploads))
	for _, file := range uploads {
		if path.Base(file.logicalName) != file.logicalName || file.logicalName == "." || file.logicalName == ".." {
			return multiDiscAttachmentError(MultiDiscAttachmentErrorSetMismatch, ErrInvalid)
		}
		folded := multidisc.ASCIIFold(file.logicalName)
		if _, duplicate := observed[folded]; duplicate {
			return multiDiscAttachmentError(MultiDiscAttachmentErrorSetMismatch, ErrInvalid)
		}
		observed[folded] = struct{}{}
	}
	for name := range expected {
		if _, exists := observed[name]; !exists {
			return multiDiscAttachmentError(MultiDiscAttachmentErrorSetMismatch, ErrInvalid)
		}
	}
	return nil
}

func (service *Service) heartbeatMultiDiscAttachment(
	ctx context.Context,
	candidate *multiDiscAttachmentCandidate,
) error {
	if err := ctx.Err(); err != nil {
		return multiDiscAttachmentStoreError("worker context", err)
	}
	now := service.now().UnixMilli()
	if err := expectOneRow(service.database.ExecContext(ctx, `
UPDATE jobs SET leased_until_ms=?,heartbeat_at_ms=?,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=? AND execution_deadline_at_ms>?
`, now+60_000, now, now, candidate.jobID, candidate.workerID, now)); err != nil {
		return multiDiscAttachmentStoreError("heartbeat", err)
	}
	return nil
}

func (service *Service) multiDiscFileForValidation(
	ctx context.Context,
	candidate *multiDiscAttachmentCandidate,
	file attachedMultiDiscFile,
) (multidisc.File, error) {
	if file.blobSize < 8 {
		return multidisc.File{}, multiDiscAttachmentError(MultiDiscAttachmentErrorContentInvalid, ErrInvalid)
	}
	if err := service.heartbeatMultiDiscAttachment(ctx, candidate); err != nil {
		return multidisc.File{}, err
	}
	reader, err := service.blobs.OpenDigest(file.blobSHA)
	if err != nil {
		return multidisc.File{}, multiDiscAttachmentStoreError("open disc", err)
	}
	digest := sha256.New()
	buffer := make([]byte, multiDiscAttachmentReadChunk)
	header := make([]byte, 0, 8)
	var total int64
	for {
		read, readErr := reader.Read(buffer)
		if read > 0 {
			appendMultiDiscDigestChunk(digest, &header, buffer[:read])
			total += int64(read)
			if err := service.heartbeatMultiDiscAttachment(ctx, candidate); err != nil {
				cleanup.Error("close", reader.Close())
				return multidisc.File{}, err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			closeErr := reader.Close()
			return multidisc.File{}, multiDiscAttachmentStoreError("read disc", errors.Join(readErr, closeErr))
		}
	}
	if err := reader.Close(); err != nil {
		return multidisc.File{}, multiDiscAttachmentStoreError("close disc", err)
	}
	if total != file.blobSize || hex.EncodeToString(digest.Sum(nil)) != file.blobSHA {
		return multidisc.File{}, multiDiscAttachmentError(MultiDiscAttachmentErrorContentInvalid, ErrInvalid)
	}
	return multidisc.File{
		Basename: file.logicalName, LogicalName: file.logicalName,
		UploadFileID: file.uploadFileID, BlobID: file.blobID, BlobSHA256: file.blobSHA,
		SizeBytes: file.blobSize, Header: header,
	}, nil
}

func appendMultiDiscDigestChunk(digest hash.Hash, header *[]byte, chunk []byte) {
	_, _ = digest.Write(chunk)
	if len(*header) >= cap(*header) {
		return
	}
	needed := cap(*header) - len(*header)
	if needed > len(chunk) {
		needed = len(chunk)
	}
	*header = append(*header, chunk[:needed]...)
}
