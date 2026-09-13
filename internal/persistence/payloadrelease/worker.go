package payloadrelease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/dbexec"
	application "retrom/internal/service/payloadrelease"
)

type Worker struct{ database *sql.DB }

func NewWorker(database *sql.DB) *Worker { return &Worker{database: database} }
func (repository *Worker) WithWorker(ctx context.Context, run func(application.WorkerScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin release worker transaction: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := run(BindWorker(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit release worker transaction: %w", err)
	}
	return nil
}

type workerRecords struct{ executor dbexec.Executor }

func BindWorker(executor dbexec.Executor) application.WorkerScope {
	records := workerRecords{executor: executor}
	return application.WorkerScope{Read: records, Write: records, Owners: BindScheduling(executor)}
}

const workColumns = `COALESCE(job.id,''),COALESCE(job.kind,''),COALESCE(job.scope_type,''),
 COALESCE(job.scope_id,''),COALESCE(job.state,''),COALESCE(job.worker_id,''),
 COALESCE(job.execution_no,0),COALESCE(job.attempt_count,0),COALESCE(job.max_attempts,0),
 COALESCE(job.version,0),COALESCE(job.available_at_ms,0),
 job.execution_started_at_ms,job.execution_deadline_at_ms,job.leased_until_ms,job.heartbeat_at_ms,
 COALESCE(input.input_json,''),COALESCE(input.input_digest,''),input.job_id IS NOT NULL`

const workSQL = `SELECT ` + workColumns + `
 FROM jobs job LEFT JOIN job_input_snapshots input ON input.job_id=job.id AND input.execution_no=job.execution_no `

func (records workerRecords) Next(ctx context.Context, now int64) (application.Work, bool, error) {
	return readWork(records.executor.QueryRowContext(ctx, workSQL+`
 WHERE job.kind IN ('PAYLOAD_RELEASE','BLOB_GC') AND job.state='QUEUED' AND job.available_at_ms<=?
 ORDER BY job.available_at_ms,job.created_at_ms,job.id LIMIT 1`, now))
}

func (records workerRecords) Current(ctx context.Context, id string) (application.Work, bool, error) {
	return readWork(records.executor.QueryRowContext(ctx, workSQL+` WHERE job.id=?`, id))
}

func (records workerRecords) Interrupted(ctx context.Context, now int64, limit int) ([]application.Work, error) {
	rows, err := records.executor.QueryContext(ctx, workSQL+` WHERE job.kind IN ('PAYLOAD_RELEASE','BLOB_GC')
 AND job.state='RUNNING' AND (job.leased_until_ms IS NULL OR job.leased_until_ms<=?
 OR job.execution_deadline_at_ms IS NULL OR job.execution_deadline_at_ms<=?) ORDER BY job.id LIMIT ?`, now, now, limit)
	if err != nil {
		return nil, fmt.Errorf("read interrupted release work: %w", err)
	}
	defer func() { cleanup.Error("close interrupted release work", rows.Close()) }()
	works := make([]application.Work, 0)
	for rows.Next() {
		work, _, err := readWork(rows)
		if err != nil {
			return nil, err
		}
		works = append(works, work)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate interrupted release work: %w", err)
	}
	return works, nil
}

type workScanner interface{ Scan(...any) error }

func readWork(row workScanner) (application.Work, bool, error) {
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
	work.Started = workTime(started)
	work.Deadline = workTime(deadline)
	work.Lease = workTime(lease)
	work.Heartbeat = workTime(heartbeat)
	return work, true, nil
}

func workTime(value sql.NullInt64) application.WorkTime {
	return application.WorkTime{Value: value.Int64, Set: value.Valid}
}
