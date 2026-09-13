package payloadrelease

import (
	"context"
	"fmt"

	application "retrom/internal/service/payloadrelease"
)

func (records gcRecords) Queue(ctx context.Context, queue application.GCQueue) error {
	job := queue.Job
	result, err := records.executor.ExecContext(ctx, `INSERT INTO jobs
 (id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
 attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
 VALUES(?,'BLOB',?,'BLOB_GC',?,1,'{"inputExecutionNo":1}',0,'QUEUED',0,4,1,?,?,?)`,
		job.ID, job.Scope.ID, job.DedupeKey, queue.AvailableMS, job.NowMS, job.NowMS)
	if err := gcWrite(result, err); err != nil {
		return fmt.Errorf("create GC job: %w", err)
	}
	if err := records.input(ctx, job.ID, 1, job.InputJSON, job.InputDigest, job.NowMS); err != nil {
		return err
	}
	if err := records.event(ctx, job.ID, queue.Before.ID, "QUEUED", queue.EventJSON, job.NowMS); err != nil {
		return err
	}
	result, err = records.executor.ExecContext(ctx, `INSERT INTO blob_gc_candidates
 (blob_id,gc_job_id,first_unreferenced_at_ms,scheduled_at_ms,attempt_count) VALUES(?,?,?,?,0)`,
		queue.Before.ID, job.ID, job.NowMS, queue.AvailableMS)
	if err := gcWrite(result, err); err != nil {
		return fmt.Errorf("create GC candidate: %w", err)
	}
	return nil
}

func (records gcRecords) input(
	ctx context.Context, id string, execution int64, encoded, digest string, now int64,
) error {
	result, err := records.executor.ExecContext(ctx, `INSERT INTO job_input_snapshots
 (job_id,execution_no,input_json,input_digest,created_at_ms) VALUES(?,?,?,?,?)`, id, execution, encoded, digest, now)
	if err := gcWrite(result, err); err != nil {
		return fmt.Errorf("create GC input: %w", err)
	}
	return nil
}

func (records gcRecords) event(ctx context.Context, id, blobID, kind, data string, now int64) error {
	result, err := records.executor.ExecContext(ctx, `INSERT INTO job_events
 (job_id,scope_type,scope_id,event_type,data_json,created_at_ms) VALUES(?,'BLOB',?,?,?,?)`, id, blobID, kind, data, now)
	if err := gcWrite(result, err); err != nil {
		return fmt.Errorf("create GC event: %w", err)
	}
	return nil
}
