package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/payloadrelease"
)

func (records scheduling) CreateJob(ctx context.Context, job application.ScheduledJob) error {
	result, err := records.executor.ExecContext(ctx, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,?,?,'PAYLOAD_RELEASE',?,1,'{"inputExecutionNo":1}',0,'QUEUED',0,4,1,?,?,?)
`, job.ID, job.Scope.Type, job.Scope.ID, job.DedupeKey, job.NowMS, job.NowMS, job.NowMS)
	if err := schedulingWrite(result, err); err != nil {
		return fmt.Errorf("create payload release job: %w", err)
	}
	result, err = records.executor.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,1,?,?,?)`, job.ID, job.InputJSON, job.InputDigest, job.NowMS)
	if err := schedulingWrite(result, err); err != nil {
		return fmt.Errorf("create payload release input: %w", err)
	}
	result, err = records.executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,?,?,'QUEUED','{"schemaVersion":1,"executionNo":1,"attempt":0}',?)
`, job.ID, job.Scope.Type, job.Scope.ID, job.NowMS)
	if err := schedulingWrite(result, err); err != nil {
		return fmt.Errorf("create payload release queued event: %w", err)
	}
	return nil
}

func schedulingWrite(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("write payload scheduling record: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count payload scheduling writes: %w", err)
	}
	if rows != 1 {
		return application.ErrScopeInvalid
	}
	return nil
}
