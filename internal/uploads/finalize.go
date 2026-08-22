package uploads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
)

var (
	errFinalizeIO  = errors.New("UPLOAD_FINALIZE_IO")
	errPartMissing = errors.New("UPLOAD_PART_MISSING")
)

// Contract branches stay contiguous for a single auditable decision.
func (service *Service) Cancel(ctx context.Context, uploadID string, version int64) (Canceled, bool, error) {
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Canceled{}, false, fmt.Errorf("uploads/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state string
	var currentVersion int64
	var jobID sql.NullString
	if err := transaction.QueryRowContext(ctx, `
SELECT state,
version,
finalize_job_id
FROM upload_sessions
WHERE id=?
`, uploadID).Scan(&state, &currentVersion, &jobID); err != nil {
		return Canceled{}, false, fmt.Errorf("uploads/service: %w", err)
	}
	if currentVersion != version || state == "COMPLETE" || state == "CANCELLED" || state == "EXPIRED" {
		return Canceled{}, false, ErrInvalid
	}
	pending := state == "FINALIZING"
	newVersion := currentVersion + 1
	if pending {
		pending, err = requestFinalizeCancellation(ctx, transaction, jobID.String, now)
		if err != nil {
			return Canceled{}, false, fmt.Errorf("uploads/service: %w", err)
		}
	}
	if !pending {
		if _, err := transaction.ExecContext(ctx, `
UPDATE upload_sessions
SET state='CANCELLED',
version=?,
updated_at_ms=?
WHERE id=?;
 UPDATE upload_files
SET state='FAILED',
last_error_code='UPLOAD_CANCELLED',
updated_at_ms=?
WHERE upload_session_id=?
AND state!='COMPLETE'
`, newVersion, now, uploadID, now, uploadID); err != nil {
			return Canceled{}, false, fmt.Errorf("uploads/service: %w", err)
		}
	} else if _, err := transaction.ExecContext(ctx, `
UPDATE upload_sessions
SET version=?,
updated_at_ms=?
WHERE id=?
`, newVersion, now, uploadID); err != nil {
		return Canceled{}, false, fmt.Errorf("uploads/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Canceled{}, false, fmt.Errorf("uploads/service: %w", err)
	}
	if !pending {
		// The directory is derived from the already validated UUID route value.
		cleanup.RemoveAll(filepath.Join(service.dataDir, "tmp", "uploads", uploadID))
	}
	resultState := "CANCELLED"
	if pending {
		resultState = "CANCEL_REQUESTED"
	}
	return Canceled{UploadID: uploadID, State: resultState, Version: newVersion}, pending, nil
}

func requestFinalizeCancellation(
	ctx context.Context,
	transaction *sql.Tx,
	jobID string,
	now int64,
) (bool, error) {
	var jobState string
	if err := transaction.QueryRowContext(ctx, `
SELECT state
FROM jobs
WHERE id=?
`, jobID).Scan(&jobState); err != nil {
		return false, fmt.Errorf("read finalize job state: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state=CASE WHEN state='QUEUED' THEN 'CANCELLED' ELSE 'CANCEL_REQUESTED' END,
cancel_requested_at_ms=?,
cancel_reason='upload cancelled',
finished_at_ms=CASE WHEN state='QUEUED' THEN ? ELSE finished_at_ms END,
version=version+1,
updated_at_ms=?
WHERE id=?
AND state IN ('QUEUED',
'RUNNING')
`, now, now, now, jobID)
	if err != nil {
		return false, fmt.Errorf("request finalize cancellation: %w", err)
	}
	changed, _ := result.RowsAffected()
	return changed > 0 && jobState == "RUNNING", nil
}

type part struct {
	offset, size int64
	path         string
}

type finalizeCandidate struct {
	id   string
	size int64
}

func (service *Service) fileParts(ctx context.Context, fileID string) ([]part, error) {
	rows, err := service.database.QueryContext(
		ctx,
		`
SELECT offset_bytes,
size_bytes,
storage_key
FROM upload_parts
WHERE upload_file_id=?
ORDER BY offset_bytes
`,
		fileID,
	)
	if err != nil {
		return nil, fmt.Errorf("uploads/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	parts := make([]part, 0)
	for rows.Next() {
		var value part
		if err := rows.Scan(&value.offset, &value.size, &value.path); err != nil {
			return nil, fmt.Errorf("uploads/service: %w", err)
		}
		parts = append(parts, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("uploads/service: %w", err)
	}
	return parts, nil
}

// Contract branches stay contiguous for a single auditable decision.
func (service *Service) finalize(parent context.Context, uploadID, jobID string) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()
	now := service.now().UnixMilli()
	started, _ := service.database.ExecContext(
		ctx,
		`
UPDATE jobs
SET state='RUNNING',
attempt_count=1,
execution_started_at_ms=?,
updated_at_ms=?
WHERE id=?
AND state='QUEUED'
`,
		now,
		now,
		jobID,
	)
	if rows, _ := started.RowsAffected(); rows != 1 {
		return
	}
	files, err := service.finalizeCandidates(ctx, uploadID)
	if err != nil {
		service.fail(ctx, uploadID, jobID, "UPLOAD_FINALIZE_IO")
		return
	}
	for _, file := range files {
		if service.cancelRequested(ctx, uploadID, jobID) {
			return
		}
		if err := service.finalizeCandidate(ctx, file); err != nil {
			service.fail(ctx, uploadID, jobID, err.Error())
			return
		}
	}
	if service.cancelRequested(ctx, uploadID, jobID) {
		return
	}
	service.publishFinalizedUpload(ctx, uploadID, jobID)
}

func (service *Service) finalizeCandidates(ctx context.Context, uploadID string) ([]finalizeCandidate, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT id,
declared_size_bytes
FROM upload_files
WHERE upload_session_id=?
AND state!='COMPLETE'
ORDER BY id
`, uploadID)
	if err != nil {
		return nil, fmt.Errorf("list finalize candidates: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	var files []finalizeCandidate
	for rows.Next() {
		var file finalizeCandidate
		if err := rows.Scan(&file.id, &file.size); err != nil {
			return nil, fmt.Errorf("scan finalize candidate: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate finalize candidates: %w", err)
	}
	return files, nil
}

func (service *Service) finalizeCandidate(ctx context.Context, file finalizeCandidate) error {
	parts, err := service.fileParts(ctx, file.id)
	if err != nil {
		return errFinalizeIO
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].offset < parts[j].offset })
	var offset int64
	var handles []*os.File
	var readers []io.Reader
	for _, value := range parts {
		if value.offset != offset {
			closeFinalizeHandles(handles)
			return errPartMissing
		}
		handle, openErr := os.Open(filepath.Join(service.dataDir, "tmp", "uploads", filepath.FromSlash(value.path)))
		if openErr != nil {
			closeFinalizeHandles(handles)
			return errPartMissing
		}
		handles = append(handles, handle)
		readers = append(readers, io.LimitReader(handle, value.size))
		offset += value.size
	}
	if offset != file.size {
		closeFinalizeHandles(handles)
		return errPartMissing
	}
	metadata, err := service.blobs.Put(io.MultiReader(readers...))
	closeFinalizeHandles(handles)
	if err != nil {
		return errFinalizeIO
	}
	blobID := ""
	lookupErr := service.database.QueryRowContext(ctx, `
SELECT id
FROM blobs
WHERE sha256=?
`, metadata.SHA256).
		Scan(&blobID)
	if errors.Is(lookupErr, sql.ErrNoRows) {
		generated, _ := uuid.NewV7()
		blobID = generated.String()
		if _, insertErr := service.database.ExecContext(ctx, `
INSERT INTO blobs(id,
sha256,
size_bytes,
md5,
sha1,
crc32,
media_type,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?)
`,
			blobID, metadata.SHA256, metadata.Size, metadata.MD5, metadata.SHA1,
			metadata.CRC32, "application/octet-stream", service.now().UnixMilli(),
		); insertErr != nil {
			return errFinalizeIO
		}
	} else if lookupErr != nil {
		return errFinalizeIO
	}
	if _, err := service.database.ExecContext(ctx, `
UPDATE upload_files
SET final_blob_id=?,
state='COMPLETE',
last_error_code=NULL,
updated_at_ms=?
WHERE id=?
`, blobID, service.now().UnixMilli(), file.id); err != nil {
		return errFinalizeIO
	}
	return nil
}

func closeFinalizeHandles(handles []*os.File) {
	for _, handle := range handles {
		cleanup.Error("close", handle.Close())
	}
}

func (service *Service) publishFinalizedUpload(ctx context.Context, uploadID, jobID string) {
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		service.fail(ctx, uploadID, jobID, "UPLOAD_FINALIZE_IO")
		return
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
UPDATE upload_sessions
SET state='COMPLETE',
version=version+1,
expires_at_ms=?,
last_error_code=NULL,
updated_at_ms=?
WHERE id=?
`, now+int64(7*24*time.Hour/time.Millisecond), now, uploadID); err != nil {
		cleanup.Rollback(transaction)
		service.fail(ctx, uploadID, jobID, "UPLOAD_FINALIZE_IO")
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='SUCCEEDED',
finished_at_ms=?,
updated_at_ms=?
WHERE id=?
`, now, now, jobID); err != nil {
		cleanup.Rollback(transaction)
		service.fail(ctx, uploadID, jobID, "UPLOAD_FINALIZE_IO")
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'UPLOAD_SESSION',
?,
'SUCCEEDED',
'{}',
?)
`, jobID, uploadID, now); err != nil {
		cleanup.Rollback(transaction)
		service.fail(ctx, uploadID, jobID, "UPLOAD_FINALIZE_IO")
		return
	}
	_ = transaction.Commit()
}

func (service *Service) cancelRequested(ctx context.Context, uploadID, jobID string) bool {
	var state string
	if err := service.database.QueryRowContext(ctx, `
SELECT state
FROM jobs
WHERE id=?
`, jobID).Scan(&state); err != nil ||
		state != "CANCEL_REQUESTED" {
		return false
	}
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return true
	}
	defer cleanup.Rollback(transaction)
	_, err = transaction.ExecContext(
		ctx,
		`
UPDATE jobs
SET state='CANCELLED',
finished_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='CANCEL_REQUESTED';
 UPDATE upload_sessions
SET state='CANCELLED',
version=version+1,
updated_at_ms=?
WHERE id=?;
 UPDATE upload_files
SET state='FAILED',
last_error_code='UPLOAD_CANCELLED',
updated_at_ms=?
WHERE upload_session_id=?
AND state!='COMPLETE';
 INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'UPLOAD_SESSION',
?,
'CANCELLED',
'{}',
?)
`,
		now,
		now,
		jobID,
		now,
		uploadID,
		now,
		uploadID,
		jobID,
		uploadID,
		now,
	)
	if err != nil || transaction.Commit() != nil {
		return true
	}
	cleanup.RemoveAll(filepath.Join(service.dataDir, "tmp", "uploads", uploadID))
	return true
}

func (service *Service) fail(ctx context.Context, uploadID, jobID, code string) {
	now := service.now().UnixMilli()
	_, _ = service.database.ExecContext(
		ctx,
		`
UPDATE upload_sessions
SET state='FAILED',
version=version+1,
last_error_code=?,
updated_at_ms=?
WHERE id=?
`,
		code,
		now,
		uploadID,
	)
	_, _ = service.database.ExecContext(
		ctx,
		`
UPDATE upload_files
SET state='FAILED',
last_error_code=?,
updated_at_ms=?
WHERE upload_session_id=?
AND state!='COMPLETE'
`,
		code,
		now,
		uploadID,
	)
	_, _ = service.database.ExecContext(
		ctx,
		`
UPDATE jobs
SET state='FAILED',
error_code=?,
error_retryable=0,
finished_at_ms=?,
updated_at_ms=?
WHERE id=?
`,
		code,
		now,
		now,
		jobID,
	)
}
