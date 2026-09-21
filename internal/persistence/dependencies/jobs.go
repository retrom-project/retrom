package dependencies

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/dependencies"
	service "retrom/internal/service/dependencies"
)

func (records jobRecords) Find(ctx context.Context, dedupe string) (service.Job, bool, error) {
	var job service.Job
	err := records.executor.QueryRowContext(ctx, `
SELECT id,
state
FROM jobs
WHERE kind='DAT_PARSE'
AND dedupe_key=?
`, dedupe).Scan(&job.ID, &job.State)
	if errors.Is(err, sql.ErrNoRows) {
		return service.Job{}, false, nil
	}
	if err != nil {
		return service.Job{}, false, fmt.Errorf("dependencies/find DAT job: %w", err)
	}
	return job, true, nil
}

func (records jobRecords) Create(ctx context.Context, input service.JobCreation) error {
	if _, err := records.executor.ExecContext(ctx, `
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
'DAT_VERSION',
?,
'DAT_PARSE',
?,
1,
?,
0,
'QUEUED',
0,
2,
?,
?,
?)
`, input.ID, input.DATID, input.DedupeKey, string(input.Payload), input.AtMS, input.AtMS, input.AtMS); err != nil {
		return fmt.Errorf("dependencies/insert DAT job: %w", err)
	}
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,
execution_no,
input_json,
input_digest,
created_at_ms) VALUES(?,
1,
?,
?,
?)
`, input.ID, string(input.Input), input.InputDigest, input.AtMS); err != nil {
		return fmt.Errorf("dependencies/insert DAT job input: %w", err)
	}
	return records.event(ctx, input.ID, input.DATID, "QUEUED", input.Event, input.AtMS)
}

func (records jobRecords) Requeue(ctx context.Context, id string, now int64) error {
	if _, err := records.executor.ExecContext(ctx, `
UPDATE jobs
SET state='QUEUED',
execution_started_at_ms=NULL,
execution_deadline_at_ms=NULL,
leased_until_ms=NULL,
heartbeat_at_ms=NULL,
worker_id=NULL,
version=version+1,
updated_at_ms=?
WHERE id=?
`, now, id); err != nil {
		return fmt.Errorf("dependencies/requeue DAT job: %w", err)
	}
	return nil
}

func (records jobRecords) Claim(ctx context.Context, input service.JobClaim) error {
	if err := requireJobChange(records.executor.ExecContext(ctx, `
UPDATE jobs
SET state='RUNNING',
attempt_count=attempt_count+1,
execution_started_at_ms=COALESCE(execution_started_at_ms,
?),
execution_deadline_at_ms=COALESCE(execution_deadline_at_ms,
?),
leased_until_ms=?,
heartbeat_at_ms=?,
worker_id='builtin-dat-indexer',
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='QUEUED'
`, input.AtMS, input.DeadlineMS, input.LeaseUntilMS,
		input.AtMS, input.AtMS, input.JobID)); err != nil {
		return err
	}
	return records.event(ctx, input.JobID, input.DATID, "STARTED", input.Event, input.AtMS)
}

func (records jobRecords) Finish(ctx context.Context, input service.JobFinish) error {
	var err error
	switch input.State {
	case "SUCCEEDED":
		err = requireJobChange(records.executor.ExecContext(ctx, `
UPDATE jobs
SET state='SUCCEEDED',
finished_at_ms=?,
leased_until_ms=NULL,
heartbeat_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='RUNNING'
`, input.AtMS, input.AtMS, input.AtMS, input.JobID))
	case "FAILED":
		err = requireJobChange(records.executor.ExecContext(ctx, `
UPDATE jobs
SET state='FAILED',
error_code=?,
error_retryable=0,
finished_at_ms=?,
leased_until_ms=NULL,
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='RUNNING'
`, input.Code, input.AtMS, input.AtMS, input.JobID))
	default:
		return dependencies.ErrInvalid
	}
	if err != nil {
		return err
	}
	return records.event(ctx, input.JobID, input.DATID, input.State, input.Event, input.AtMS)
}

func (records jobRecords) event(ctx context.Context, jobID, datID, kind string, payload []byte, now int64) error {
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'DAT_VERSION',?,?,?,?)
`, jobID, datID, kind, string(payload), now); err != nil {
		return fmt.Errorf("dependencies/write DAT job event: %w", err)
	}
	return nil
}

func requireJobChange(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("dependencies/write DAT job state: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("dependencies/count DAT job changes: %w", err)
	}
	if changed != 1 {
		return service.ErrDATJobNotClaimed
	}
	return nil
}
