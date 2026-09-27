package cleanupjobs

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/jobrecord"
	application "retrom/internal/service/cleanupjobs"
)

type Worker struct{ database dbapi.DB }

func NewWorker(database dbapi.DB) *Worker { return &Worker{database: database} }
func (repository *Worker) WithWorker(ctx context.Context, run func(application.WorkerScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin release worker transaction: %w", err)
	}
	defer dbapi.Rollback(tx)
	if err := run(BindWorker(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit release worker transaction: %w", err)
	}
	return nil
}

type workerRecords struct{ executor dbapi.Executor }

func BindWorker(executor dbapi.Executor) application.WorkerScope {
	records := workerRecords{executor: executor}
	return application.WorkerScope{Read: records, Write: records}
}

const workColumns = jobrecord.Columns

const workSQL = `SELECT ` + workColumns + `
 FROM jobs job LEFT JOIN job_input_snapshots input ON input.job_id=job.id AND input.execution_no=job.execution_no `

func (records workerRecords) Next(ctx context.Context, now int64) (application.Work, bool, error) {
	return readWork(dbapi.QueryRowContext(ctx, records.executor, workSQL+`
 WHERE job.kind IN ('OWNER_CLEANUP','PATH_DELETE') AND job.state='QUEUED' AND job.available_at_ms<=?
 ORDER BY job.available_at_ms,job.created_at_ms,job.id LIMIT 1`, now))
}

func (records workerRecords) Current(ctx context.Context, id string) (application.Work, bool, error) {
	return readWork(dbapi.QueryRowContext(ctx, records.executor, workSQL+` WHERE job.id=?`, id))
}

func (records workerRecords) Interrupted(ctx context.Context, now int64, limit int) ([]application.Work, error) {
	rows, err := records.executor.QueryContext(
		ctx,
		workSQL+` WHERE job.kind IN ('OWNER_CLEANUP','PATH_DELETE')
 AND job.state='RUNNING' AND (job.leased_until_ms IS NULL OR job.leased_until_ms<=?
 OR job.execution_deadline_at_ms IS NULL OR job.execution_deadline_at_ms<=?) ORDER BY job.id LIMIT ?`,
		now,
		now,
		limit,
	)
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

var readWork = jobrecord.Read
