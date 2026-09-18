package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/launch"
	"retrom/internal/repo/dbexec"
)

type (
	ValidationWorker        struct{ database *sql.DB }
	validationWorkerRecords struct{ executor dbexec.Executor }
)

func NewValidationWorker(database *sql.DB) *ValidationWorker {
	return &ValidationWorker{database: database}
}

func (repository *ValidationWorker) LoadValidationWork(
	ctx context.Context,
	id string,
) (application.ValidationWork, bool, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ValidationWork{}, false, fmt.Errorf("begin validation read: %w", err)
	}
	defer dbexec.Rollback(tx)
	work, found, err := (validationWorkerRecords{executor: tx}).Read(ctx, id)
	if err != nil {
		return application.ValidationWork{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return application.ValidationWork{}, false, fmt.Errorf("commit validation read: %w", err)
	}
	return work, found, nil
}

func (repository *ValidationWorker) LoadValidationFacts(
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

func (repository *ValidationWorker) commitValidationTx(
	ctx context.Context,
	label string,
	work func(validationWorkerRecords) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin %s: %w", label, err)
	}
	defer dbexec.Rollback(tx)
	if err := work(validationWorkerRecords{executor: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", label, err)
	}
	return nil
}

func (repository *ValidationWorker) CommitValidationClaim(
	ctx context.Context,
	plan application.ValidationClaimWrite,
) error {
	return repository.commitValidationTx(ctx, "validation claim", func(r validationWorkerRecords) error {
		return r.Claim(ctx, plan)
	})
}

func (repository *ValidationWorker) CommitValidationRenewal(
	ctx context.Context,
	claim application.ValidationClaim,
	nowMS, leaseMS int64,
) error {
	return repository.commitValidationTx(ctx, "validation renewal", func(r validationWorkerRecords) error {
		return r.Renew(ctx, claim, nowMS, leaseMS)
	})
}

func (repository *ValidationWorker) CommitValidationFinish(
	ctx context.Context,
	plan application.ValidationTerminal,
) error {
	return repository.commitValidationTx(ctx, "validation finish", func(r validationWorkerRecords) error {
		return r.Finish(ctx, plan)
	})
}

func (repository *ValidationWorker) CommitValidationRecovery(
	ctx context.Context,
	plan application.ValidationRecovery,
) error {
	return repository.commitValidationTx(ctx, "validation recovery", func(r validationWorkerRecords) error {
		return r.Recover(ctx, plan)
	})
}

func (repository *ValidationWorker) CommitValidationSettlement(
	ctx context.Context,
	settlement application.ValidationSettlement,
) error {
	return repository.commitValidationTx(ctx, "validation settlement", func(r validationWorkerRecords) error {
		if err := r.Apply(ctx, settlement.Variant); err != nil {
			return err
		}
		return r.Finish(ctx, settlement.Finish)
	})
}

func (repository *ValidationWorker) LoadValidationCandidates(ctx context.Context, now int64) ([]string, error) {
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
