package metadatascrape

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/persistence/dbexec"
	"retrom/internal/service/metadatascrape"
)

type (
	WorkerRepository struct{ database *sql.DB }
	workerRecords    struct{ transaction *sql.Tx }
)

func NewWorker(database *sql.DB) *WorkerRepository { return &WorkerRepository{database} }
func (repository *WorkerRepository) Run(ctx context.Context, id string) (metadatascrape.WorkerRun, error) {
	var run metadatascrape.WorkerRun
	err := repository.database.QueryRowContext(
		ctx,
		`SELECT r.id,r.job_id,r.provider,r.state,j.state,j.payload_json,j.execution_no,
 j.version,j.attempt_count,j.max_attempts,
 COALESCE(j.execution_deadline_at_ms,0),COALESCE(j.leased_until_ms,0),j.available_at_ms
 FROM metadata_scrape_runs r JOIN jobs j ON j.id=r.job_id WHERE r.id=?`,
		id,
	).
		Scan(&run.RunID, &run.JobID, &run.Provider, &run.State, &run.JobState, &run.Payload, &run.ExecutionNo,
			&run.Version, &run.AttemptCount, &run.MaxAttempts, &run.Deadline, &run.LeaseUntil, &run.AvailableAt)
	if err != nil {
		return run, fmt.Errorf("query metadata execution: %w", err)
	}
	return run, nil
}

func (repository *WorkerRepository) WithWrite(ctx context.Context, work func(metadatascrape.WorkerScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin metadata execution: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := workerRecords{tx}
	if err := work(
		metadatascrape.WorkerScope{
			Leases: records,
			Write:  records,
			Initial: BindInitialReview(
				tx,
			),
		},
	); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit metadata execution: %w", err)
	}
	return nil
}

func workerChanged(result sql.Result, err error) (bool, error) {
	if err != nil {
		return false, fmt.Errorf("update metadata execution: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count metadata executions: %w", err)
	}
	return n == 1, nil
}

func (records workerRecords) event(ctx context.Context, id, kind, data string, now int64) error {
	_, err := records.transaction.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
 SELECT id,scope_type,scope_id,?,?,? FROM jobs WHERE id=?`,
		kind,
		data,
		now,
		id,
	)
	if err != nil {
		return fmt.Errorf("insert metadata execution event: %w", err)
	}
	return nil
}
