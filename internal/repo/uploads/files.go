package uploads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/repo/blobcatalog"
	"retrom/internal/repo/dbexec"
	service "retrom/internal/service/uploads"
)

func (repository *Repository) Target(ctx context.Context, key service.FileKey) (service.PartTarget, error) {
	return fileRecords{repository.database}.Target(ctx, key)
}

func (records fileRecords) Target(ctx context.Context, key service.FileKey) (service.PartTarget, error) {
	result := service.PartTarget{FileKey: key}
	var code sql.NullString
	err := records.executor.QueryRowContext(
		ctx,
		`
SELECT file.declared_size_bytes,file.state,session.state,session.version,session.expires_at_ms,file.last_error_code
FROM upload_files file JOIN upload_sessions session ON session.id=file.upload_session_id
WHERE file.id=? AND session.id=?
`,
		key.FileID,
		key.UploadID,
	).Scan(
		&result.DeclaredSize,
		&result.FileState,
		&result.SessionState,
		&result.SessionVersion,
		&result.ExpiresAtMS,
		&code,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return service.PartTarget{}, service.ErrNotFound
	}
	if err != nil {
		return service.PartTarget{}, fmt.Errorf("uploads/read part target: %w", err)
	}
	result.LastErrorCode = dbexec.StringPointer(code)
	return result, nil
}

func (records fileRecords) AddReceived(ctx context.Context, input service.FileProgress) error {
	return requireChange(records.executor.ExecContext(ctx, `
UPDATE upload_files SET received_size_bytes=received_size_bytes+?,state='PARTIAL',updated_at_ms=? WHERE id=?
`, input.Bytes, input.AtMS, input.FileID))
}

func (records fileRecords) MarkFinalizing(ctx context.Context, id string, now int64) error {
	if _, err := records.executor.ExecContext(ctx, `
UPDATE upload_files SET state='FINALIZING',updated_at_ms=? WHERE upload_session_id=? AND state!='COMPLETE'
`, now, id); err != nil {
		return fmt.Errorf("uploads/mark files finalizing: %w", err)
	}
	return nil
}

func (records fileRecords) Publish(ctx context.Context, input service.FilePublication) error {
	return requireChange(
		records.executor.ExecContext(
			ctx,
			`
UPDATE upload_files SET final_blob_id=?,state='COMPLETE',last_error_code=NULL,updated_at_ms=?
WHERE id=? AND upload_session_id=? AND state='FINALIZING'
 AND EXISTS(SELECT 1 FROM upload_sessions session JOIN jobs job ON job.id=session.finalize_job_id
 WHERE session.id=upload_files.upload_session_id AND session.state='FINALIZING' AND session.finalization_no=?
 AND job.id=? AND job.execution_no=? AND job.state='RUNNING' AND job.worker_id=?
 AND job.attempt_count=? AND job.leased_until_ms>? AND job.execution_deadline_at_ms>?)
`,
			input.BlobID,
			input.AtMS,
			input.FileID,
			input.Run.UploadID,
			input.Run.FinalizationNo,
			input.Run.JobID,
			input.Run.ExecutionNo,
			input.Run.WorkerID, input.Run.Attempt, input.AtMS, input.AtMS,
		),
	)
}

func (records fileRecords) FailPending(ctx context.Context, input service.PendingFailure) error {
	if _, err := records.executor.ExecContext(ctx, `
UPDATE upload_files SET state='FAILED',last_error_code=?,updated_at_ms=? WHERE upload_session_id=? AND
state!='COMPLETE'
`, input.Code, input.AtMS, input.UploadID); err != nil {
		return fmt.Errorf("uploads/fail unfinished files: %w", err)
	}
	return nil
}

func (records blobRecords) Ensure(ctx context.Context, metadata blobstore.Metadata, now int64) (string, error) {
	id, err := blobcatalog.EnsureRecord(ctx, records.executor, metadata, "application/octet-stream", now)
	if err != nil {
		return "", fmt.Errorf("uploads/register blob: %w", err)
	}
	return id, nil
}
