package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/dbexec"
	application "retrom/internal/service/launch"
)

type (
	ValidationWorker        struct{ database *sql.DB }
	validationWorkerRecords struct{ executor dbexec.Executor }
)

func NewValidationWorker(database *sql.DB) *ValidationWorker {
	return &ValidationWorker{database: database}
}

func (repository *ValidationWorker) WithWorker(
	ctx context.Context,
	work func(application.ValidationWorkerScope) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin validation worker: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := validationWorkerRecords{executor: tx}
	if err := work(application.ValidationWorkerScope{Jobs: records, Facts: records, Variants: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit validation worker: %w", err)
	}
	return nil
}

func (repository *ValidationWorker) Facts(
	ctx context.Context,
	inputs application.ValidationInputs,
) (application.ValidationFacts, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ValidationFacts{}, fmt.Errorf("begin validation facts: %w", err)
	}
	defer dbexec.Rollback(tx)
	facts, err := (validationWorkerRecords{executor: tx}).Facts(ctx, inputs)
	if err != nil {
		return application.ValidationFacts{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.ValidationFacts{}, fmt.Errorf("commit validation facts: %w", err)
	}
	return facts, nil
}

func (repository *ValidationWorker) Candidates(ctx context.Context, now int64) ([]string, error) {
	rows, err := repository.database.QueryContext(ctx, `SELECT id FROM jobs WHERE kind='VARIANT_VALIDATE'
 AND ((state='QUEUED' AND available_at_ms<=?) OR (state='RUNNING' AND leased_until_ms<=?))
 ORDER BY created_at_ms,id`, now, now)
	if err != nil {
		return nil, fmt.Errorf("query validation candidates: %w", err)
	}
	defer func() { cleanup.Error("close validation candidates", rows.Close()) }()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan validation candidate: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate validation candidates: %w", err)
	}
	return ids, nil
}

func (records validationWorkerRecords) Read(ctx context.Context, id string) (application.ValidationWork, bool, error) {
	var work application.ValidationWork
	err := records.executor.QueryRowContext(ctx, `SELECT job.id,job.kind,job.scope_type,job.scope_id,job.state,
 COALESCE(job.worker_id,''),COALESCE(snapshot.input_json,''),COALESCE(snapshot.input_digest,''),job.version,
 job.execution_no,job.attempt_count,job.max_attempts,job.available_at_ms,
 job.execution_started_at_ms,job.execution_deadline_at_ms,job.leased_until_ms
 FROM jobs job LEFT JOIN job_input_snapshots snapshot
 ON snapshot.job_id=job.id AND snapshot.execution_no=job.execution_no
 WHERE job.id=?`, id).Scan(&work.ID, &work.Kind, &work.ScopeType, &work.ScopeID, &work.State, &work.WorkerID,
		&work.SnapshotJSON,
		&work.InputDigest,
		&work.Version,
		&work.ExecutionNo,
		&work.Attempt,
		&work.MaxAttempts,
		&work.AvailableMS,
		&work.StartedMS, &work.DeadlineMS, &work.LeaseMS)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ValidationWork{}, false, nil
	}
	if err != nil {
		return application.ValidationWork{}, false, fmt.Errorf("read validation execution: %w", err)
	}
	return work, true, nil
}

func validationAffected(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("write validation execution: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count validation execution: %w", err)
	}
	if affected != 1 {
		return application.ErrValidationOwnership
	}
	return nil
}
