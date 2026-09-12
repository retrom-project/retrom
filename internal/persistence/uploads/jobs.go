package uploads

import (
	"context"
	"fmt"

	service "retrom/internal/service/uploads"
)

func (records jobRecords) Create(ctx context.Context, input service.JobCreation) error {
	if _, err := records.executor.ExecContext(
		ctx,
		`
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
 attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'UPLOAD_SESSION',?,'UPLOAD_FINALIZE',?,1,?,1,'QUEUED',0,2,?,?,?)
`,
		input.Run.JobID,
		input.Run.UploadID,
		input.DedupeKey,
		string(
			input.PayloadJSON,
		),
		input.AtMS,
		input.AtMS,
		input.AtMS,
	); err != nil {
		return fmt.Errorf("uploads/insert finalize job: %w", err)
	}
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms) VALUES(?,1,?,?,?)
`, input.Run.JobID, string(input.InputJSON), input.InputDigest, input.AtMS); err != nil {
		return fmt.Errorf("uploads/insert finalize input: %w", err)
	}
	return records.event(ctx, input.Run, "QUEUED", input.EventJSON, input.AtMS)
}

func (records jobRecords) Get(ctx context.Context, id string) (service.Job, error) {
	var result service.Job
	if err := records.executor.QueryRowContext(ctx, `SELECT id,state,execution_no FROM jobs WHERE id=?`, id).
		Scan(&result.ID, &result.State, &result.ExecutionNo); err != nil {
		return service.Job{}, fmt.Errorf("uploads/read finalize job: %w", err)
	}
	return result, nil
}

func (records jobRecords) Claim(ctx context.Context, input service.JobClaim) (bool, error) {
	run, now := input.Run, input.AtMS
	result, err := records.executor.ExecContext(ctx, `
UPDATE jobs SET state='RUNNING',attempt_count=attempt_count+1,execution_started_at_ms=?,
version=version+1,updated_at_ms=?
WHERE id=? AND execution_no=? AND state='QUEUED'
`, now, now, run.JobID, run.ExecutionNo)
	if err != nil {
		return false, fmt.Errorf("uploads/claim finalize job: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("uploads/count claimed jobs: %w", err)
	}
	if count != 1 {
		return false, nil
	}
	if err := records.event(ctx, run, "STARTED", input.EventJSON, now); err != nil {
		return false, err
	}
	return true, nil
}

func (records jobRecords) RequestCancel(ctx context.Context, input service.JobCancellation) error {
	if err := requireChange(
		records.executor.ExecContext(
			ctx,
			`
UPDATE jobs SET state=?,cancel_requested_at_ms=?,cancel_reason='upload cancelled',
 finished_at_ms=COALESCE(?,finished_at_ms),version=version+1,updated_at_ms=?
WHERE id=? AND state=? AND execution_no=?
`,
			input.State,
			input.AtMS,
			input.FinishedAtMS,
			input.AtMS,
			input.ID,
			input.ExpectedState,
			input.ExecutionNo,
		),
	); err != nil {
		return err
	}
	return records.event(
		ctx,
		service.Run{
			UploadID: input.UploadID,
			JobID:    input.ID,
		},
		input.State,
		input.EventJSON,
		input.AtMS,
	)
}

func (records jobRecords) Finish(ctx context.Context, input service.JobFinish) error {
	if err := requireChange(
		records.executor.ExecContext(
			ctx,
			`
UPDATE jobs SET state=?,error_code=?,error_retryable=0,finished_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state=? AND execution_no=?
`,
			input.State,
			input.ErrorCode,
			input.AtMS,
			input.AtMS,
			input.Run.JobID,
			input.ExpectedState,
			input.Run.ExecutionNo,
		),
	); err != nil {
		return err
	}
	return records.event(ctx, input.Run, input.State, input.EventJSON, input.AtMS)
}

func (records jobRecords) event(ctx context.Context, run service.Run, kind string, data []byte, now int64) error {
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms) VALUES(?,
'UPLOAD_SESSION',?,?,?,?)
`, run.JobID, run.UploadID, kind, string(data), now); err != nil {
		return fmt.Errorf("uploads/insert job event: %w", err)
	}
	return nil
}
