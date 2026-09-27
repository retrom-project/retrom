package jobrecord

import (
	"database/sql"
	"errors"
	"fmt"
	application "retrom/internal/service/payloadrelease"
)

const Columns = `COALESCE(job.id,''),COALESCE(job.kind,''),COALESCE(job.scope_type,''),
 COALESCE(job.scope_id,''),COALESCE(job.state,''),COALESCE(job.worker_id,''),
 COALESCE(job.execution_no,0),COALESCE(job.attempt_count,0),COALESCE(job.max_attempts,0),
 COALESCE(job.version,0),COALESCE(job.available_at_ms,0),
 job.execution_started_at_ms,job.execution_deadline_at_ms,job.leased_until_ms,job.heartbeat_at_ms,
 COALESCE(input.input_json,''),COALESCE(input.input_digest,''),input.job_id IS NOT NULL`

type Scanner interface{ Scan(...any) error }

func Read(row Scanner) (application.Work, bool, error) {
	var work application.Work
	var started, deadline, lease, heartbeat sql.NullInt64
	err := row.Scan(&work.ID, &work.Kind, &work.Scope.Type, &work.Scope.ID, &work.State, &work.WorkerID,
		&work.ExecutionNo, &work.Attempt, &work.MaxAttempts, &work.Version, &work.AvailableMS,
		&started, &deadline, &lease, &heartbeat, &work.InputJSON, &work.InputDigest, &work.InputFound)
	if errors.Is(err, sql.ErrNoRows) {
		return application.Work{}, false, nil
	}
	if err != nil {
		return application.Work{}, false, fmt.Errorf("decode release work: %w", err)
	}
	work.Started = WorkTime(started)
	work.Deadline = WorkTime(deadline)
	work.Lease = WorkTime(lease)
	work.Heartbeat = WorkTime(heartbeat)
	return work, true, nil
}

func WorkTime(value sql.NullInt64) application.WorkTime {
	return application.WorkTime{Value: value.Int64, Set: value.Valid}
}

const Fence = ` WHERE id=? AND kind=? AND scope_type=? AND scope_id=? AND state=?
 AND COALESCE(worker_id,'')=? AND execution_no=? AND attempt_count=? AND max_attempts=? AND version=?
 AND available_at_ms=? AND COALESCE(execution_started_at_ms,-1)=? AND COALESCE(execution_deadline_at_ms,-1)=?
 AND COALESCE(leased_until_ms,-1)=? AND COALESCE(heartbeat_at_ms,-1)=?
 AND ((?=1 AND EXISTS(SELECT 1 FROM job_input_snapshots input WHERE input.job_id=jobs.id
 AND input.execution_no=jobs.execution_no AND input.input_json=? AND input.input_digest=?))
 OR (?=0 AND NOT EXISTS(SELECT 1 FROM job_input_snapshots input
 WHERE input.job_id=jobs.id AND input.execution_no=jobs.execution_no)))`

func Arguments(work application.Work) []any {
	return []any{
		work.ID, work.Kind, work.Scope.Type, work.Scope.ID, work.State, work.WorkerID, work.ExecutionNo,
		work.Attempt, work.MaxAttempts, work.Version, work.AvailableMS, timeFence(work.Started), timeFence(work.Deadline),
		timeFence(work.Lease), timeFence(work.Heartbeat), work.InputFound, work.InputJSON, work.InputDigest, work.InputFound,
	}
}

func timeFence(value application.WorkTime) int64 {
	if !value.Set {
		return -1
	}
	return value.Value
}
