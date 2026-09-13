package uploads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/repo/dbexec"
	service "retrom/internal/service/uploads"
)

func (records sessionRecords) Create(ctx context.Context, input service.Registration) error {
	session := input.Session
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO upload_sessions(id,purpose,state,source_type,total_files,total_bytes,manifest_digest,
expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,?,'CREATED',?,?,?,?,?,?,?)
`, session.ID, session.Purpose, session.SourceType, len(session.Files), session.TotalBytes, input.ManifestDigest,
		session.ExpiresAtMS, input.AtMS, input.AtMS); err != nil {
		return fmt.Errorf("uploads/insert session: %w", err)
	}
	for _, file := range session.Files {
		if _, err := records.executor.ExecContext(ctx, `
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,state,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,'PENDING',?,?)
`, file.ID, session.ID, file.RelativePath, file.SizeBytes, input.AtMS, input.AtMS); err != nil {
			return fmt.Errorf("uploads/insert declared file: %w", err)
		}
	}
	return nil
}

func (records sessionRecords) Current(ctx context.Context, id string) (service.SessionState, error) {
	var current service.SessionState
	var job sql.NullString
	err := records.executor.QueryRowContext(ctx, `
SELECT id,state,version,finalization_no,finalize_job_id,
 EXISTS(SELECT 1 FROM upload_consumptions WHERE upload_session_id=upload_sessions.id)
FROM upload_sessions WHERE id=?
`, id).Scan(&current.ID, &current.State, &current.Version, &current.FinalizationNo, &job, &current.Consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return service.SessionState{}, service.ErrNotFound
	}
	if err != nil {
		return service.SessionState{}, fmt.Errorf("uploads/read session state: %w", err)
	}
	current.FinalizeJobID = dbexec.StringPointer(job)
	return current, nil
}

func (records sessionRecords) BeginFinalization(ctx context.Context, input service.Finalization) error {
	return requireChange(records.executor.ExecContext(ctx, `
UPDATE upload_sessions SET state='FINALIZING',finalization_no=?,finalize_job_id=?,version=version+1,updated_at_ms=?
WHERE id=? AND version=?
`, input.Run.FinalizationNo, input.Run.JobID, input.AtMS, input.Run.UploadID, input.ExpectedVersion))
}

func (records sessionRecords) Advance(ctx context.Context, input service.SessionProgress) error {
	return requireChange(records.executor.ExecContext(ctx, `
UPDATE upload_sessions SET state=?,version=version+1,updated_at_ms=? WHERE id=? AND version=?
`, input.State, input.AtMS, input.ID, input.ExpectedVersion))
}

func (records sessionRecords) Finish(ctx context.Context, input service.SessionFinish) error {
	return requireChange(records.executor.ExecContext(ctx, `
UPDATE upload_sessions SET state=?,version=version+1,last_error_code=?,updated_at_ms=?,
expires_at_ms=COALESCE(?,expires_at_ms)
WHERE id=? AND version=? AND (?='' OR (finalize_job_id=? AND finalization_no=?))
`, input.State, input.ErrorCode, input.AtMS, input.ExpiresAtMS, input.Run.UploadID, input.ExpectedVersion,
		input.Run.JobID, input.Run.JobID, input.Run.FinalizationNo))
}
