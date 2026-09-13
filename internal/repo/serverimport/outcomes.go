package serverimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/dbexec"
	"retrom/internal/service/serverimport"
)

type Outcomes struct{ database *sql.DB }

func NewOutcomes(database *sql.DB) *Outcomes { return &Outcomes{database} }
func (repository *Outcomes) WithWrite(ctx context.Context, work func(serverimport.OutcomeScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin import outcome: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := outcomeRecords{tx}
	if err := work(serverimport.OutcomeScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit import outcome: %w", err)
	}
	return nil
}

type outcomeRecords struct{ executor dbexec.Executor }

func (records outcomeRecords) Lock(
	ctx context.Context,
	unit serverimport.Work,
	now int64,
	access serverimport.WorkerAccess,
) error {
	return LockWorker(ctx, records.executor, unit, now, access)
}

func (repository *Outcomes) Recovery(ctx context.Context, now int64) (serverimport.RecoveryWork, bool, error) {
	var result serverimport.RecoveryWork
	err := repository.database.QueryRowContext(
		ctx,
		`SELECT import.id,import.job_id,job.execution_no,
 COALESCE(job.worker_id,''),job.state='CANCEL_REQUESTED'
 FROM server_imports import JOIN jobs job ON job.id=import.job_id
 WHERE (import.state='CANCEL_REQUESTED' AND job.state='CANCEL_REQUESTED') OR
 (import.state='RUNNING' AND job.state='RUNNING' AND job.attempt_count>=job.max_attempts
 AND job.leased_until_ms IS NOT NULL AND job.leased_until_ms<=?)
 ORDER BY import.updated_at_ms,import.id LIMIT 1`,
		now,
	).Scan(
		&result.Unit.ImportID,
		&result.Unit.JobID,
		&result.Unit.Execution,
		&result.Unit.Owner,
		&result.Cancelled,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return serverimport.RecoveryWork{}, false, nil
	}
	if err != nil {
		return serverimport.RecoveryWork{}, false, fmt.Errorf("read pending import recovery: %w", err)
	}
	result.Unit.Recovery = !result.Cancelled
	return result, true, nil
}

func (records outcomeRecords) Budget(ctx context.Context, unit serverimport.Work) (serverimport.RetryBudget, error) {
	var result serverimport.RetryBudget
	err := records.executor.QueryRowContext(
		ctx,
		`SELECT attempt_count,max_attempts,COALESCE(execution_deadline_at_ms,0)
 FROM jobs WHERE id=? AND execution_no=? AND worker_id=? AND state='RUNNING'`,
		unit.JobID,
		unit.Execution,
		unit.Owner,
	).Scan(
		&result.Attempt,
		&result.Maximum,
		&result.Deadline,
	)
	if err != nil {
		return serverimport.RetryBudget{}, fmt.Errorf("read import execution budget: %w", err)
	}
	return result, nil
}

func (records outcomeRecords) Counts(ctx context.Context, unit serverimport.Work) (map[string]int64, error) {
	rows, err := records.executor.QueryContext(
		ctx,
		`SELECT state,count(*) FROM server_bios_import_items WHERE server_import_id=? GROUP BY state`,
		unit.ImportID,
	)
	if err != nil {
		return nil, fmt.Errorf("read import item counts: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	counts := make(map[string]int64)
	for rows.Next() {
		var state string
		var count int64
		if err := rows.Scan(&state, &count); err != nil {
			return nil, fmt.Errorf("scan import item counts: %w", err)
		}
		counts[state] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate import item counts: %w", err)
	}
	return counts, nil
}

func (records outcomeRecords) event(
	ctx context.Context,
	unit serverimport.Work,
	event string,
	data []byte,
	now int64,
) error {
	if _, err := records.executor.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,
 data_json,created_at_ms) VALUES(?,'SERVER_IMPORT',?,?,?,?)`,
		unit.JobID,
		unit.ImportID,
		event,
		string(
			data,
		),
		now,
	); err != nil {
		return fmt.Errorf("append import outcome event: %w", err)
	}
	return nil
}
