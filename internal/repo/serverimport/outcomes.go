package serverimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/model/serverimport"
	"retrom/internal/repo/dbexec"
)

type Outcomes struct {
	database      *sql.DB
	preCommitHook func() error
}

func NewOutcomes(database *sql.DB) *Outcomes { return &Outcomes{database: database} }

func (o *Outcomes) WithPreCommitHook(hook func() error) *Outcomes {
	o.preCommitHook = hook
	return o
}

func (repository *Outcomes) tryCommit(tx *sql.Tx, label string) error {
	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", label, err)
	}
	return nil
}

func (repository *Outcomes) CommitItemOutcome(
	ctx context.Context, plan serverimport.ItemOutcome,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin import outcome: %w", err)
	}
	defer dbexec.Rollback(tx)

	records := outcomeRecords{tx}
	if err := records.Lock(
		ctx, plan.Unit, plan.Now, serverimport.RunningWorker,
	); err != nil {
		return fmt.Errorf("lock item outcome: %w", err)
	}
	if err := records.Item(ctx, plan); err != nil {
		return fmt.Errorf("write item outcome: %w", err)
	}

	return repository.tryCommit(tx, "import item outcome")
}

func (repository *Outcomes) CommitFinish(
	ctx context.Context, cmd serverimport.FinishCommand,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin import outcome: %w", err)
	}
	defer dbexec.Rollback(tx)

	records := outcomeRecords{tx}
	counts, err := lockedCounts(
		ctx, records, cmd.Unit, cmd.Now, serverimport.RunningWorker,
	)
	if err != nil {
		return err
	}
	if counts["PENDING"]+counts["EVALUATING"] > 0 {
		return serverimport.ErrOutcomeIncomplete
	}

	totals := terminalCounts(counts)
	state := "COMPLETED"
	if totals.Failed > 0 {
		state = "PARTIAL_FAILURE"
	}
	phase := "QUEUEING_REVALIDATION"
	now := cmd.Now
	if err := records.Final(ctx, serverimport.FinalOutcome{
		Unit:        cmd.Unit,
		State:       state,
		JobState:    "SUCCEEDED",
		EventType:   "SUCCEEDED",
		Phase:       &phase,
		HeartbeatAt: &now,
		Counts:      totals,
		Event:       []byte(`{"schemaVersion":1}`),
		Now:         cmd.Now,
	}); err != nil {
		return fmt.Errorf("write terminal import: %w", err)
	}

	return repository.tryCommit(tx, "import finish")
}

func (repository *Outcomes) CommitCancelOutcome(
	ctx context.Context, cmd serverimport.CancelOutcomeCommand,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin import outcome: %w", err)
	}
	defer dbexec.Rollback(tx)

	records := outcomeRecords{tx}
	counts, err := lockedCounts(
		ctx, records, cmd.Unit, cmd.Now, serverimport.CancelledWorker,
	)
	if err != nil {
		return err
	}
	counts = movePending(counts, "CANCELLED")
	if err := records.Final(ctx, serverimport.FinalOutcome{
		Unit:         cmd.Unit,
		State:        "CANCELLED",
		JobState:     "CANCELLED",
		EventType:    "CANCELLED",
		PendingState: "CANCELLED",
		PendingCode:  "CANCELLED",
		Counts:       terminalCounts(counts),
		Event:        []byte(`{"schemaVersion":1}`),
		Now:          cmd.Now,
	}); err != nil {
		return fmt.Errorf("write terminal import: %w", err)
	}

	return repository.tryCommit(tx, "import cancel outcome")
}

func (repository *Outcomes) CommitFail(
	ctx context.Context, cmd serverimport.FailCommand,
) (serverimport.FailResult, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return serverimport.FailResult{},
			fmt.Errorf("begin import outcome: %w", err)
	}
	defer dbexec.Rollback(tx)

	records := outcomeRecords{tx}
	access := serverimport.RunningWorker
	if cmd.Unit.Recovery {
		access = serverimport.ExhaustedWorker
	}
	counts, err := lockedCounts(ctx, records, cmd.Unit, cmd.Now, access)
	if err != nil {
		return serverimport.FailResult{}, err
	}

	retryable := cmd.Code == "SERVER_IMPORT_ROOT_UNAVAILABLE" ||
		cmd.Code == "INTERNAL_ERROR"
	if retryable && !cmd.Unit.Recovery {
		result, done, retryErr := repository.attemptRetry(
			ctx, tx, records, cmd, counts,
		)
		if done {
			return result, retryErr
		}
	}

	event, err := json.Marshal(
		map[string]any{"schemaVersion": 1, "code": cmd.Code},
	)
	if err != nil {
		return serverimport.FailResult{},
			fmt.Errorf("encode import failure: %w", err)
	}
	counts = movePending(counts, "COMMIT_FAILED")
	if err := records.Final(ctx, serverimport.FinalOutcome{
		Unit:         cmd.Unit,
		State:        "FAILED",
		JobState:     "FAILED",
		EventType:    "FAILED",
		Code:         &cmd.Code,
		Retryable:    &retryable,
		PendingState: "COMMIT_FAILED",
		PendingCode:  cmd.Code,
		Counts:       terminalCounts(counts),
		Event:        event,
		Now:          cmd.Now,
	}); err != nil {
		return serverimport.FailResult{},
			fmt.Errorf("write terminal import: %w", err)
	}

	if err := repository.tryCommit(tx, "import fail"); err != nil {
		return serverimport.FailResult{}, err
	}
	return serverimport.FailResult{}, nil
}

func (repository *Outcomes) attemptRetry(
	ctx context.Context,
	tx *sql.Tx,
	records outcomeRecords,
	cmd serverimport.FailCommand,
	counts map[string]int64,
) (serverimport.FailResult, bool, error) {
	retryAt, err := retryExecution(
		ctx, records, cmd.Unit, counts, cmd.Code, cmd.Now,
	)
	if err != nil {
		return serverimport.FailResult{}, true, err
	}
	if retryAt == 0 {
		return serverimport.FailResult{}, false, nil
	}
	if commitErr := repository.tryCommit(tx, "import retry"); commitErr != nil {
		return serverimport.FailResult{}, true, commitErr
	}
	return serverimport.FailResult{RetryAt: retryAt}, true, nil
}

func (repository *Outcomes) Recovery(
	ctx context.Context, now int64,
) (serverimport.RecoveryWork, bool, error) {
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
		return serverimport.RecoveryWork{}, false,
			fmt.Errorf("read pending import recovery: %w", err)
	}
	result.Unit.Recovery = !result.Cancelled
	return result, true, nil
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

func (records outcomeRecords) Budget(
	ctx context.Context, unit serverimport.Work,
) (serverimport.RetryBudget, error) {
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
		return serverimport.RetryBudget{},
			fmt.Errorf("read import execution budget: %w", err)
	}
	return result, nil
}

func (records outcomeRecords) Counts(
	ctx context.Context, unit serverimport.Work,
) (map[string]int64, error) {
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
		string(data),
		now,
	); err != nil {
		return fmt.Errorf("append import outcome event: %w", err)
	}
	return nil
}

func lockedCounts(
	ctx context.Context,
	records outcomeRecords,
	unit serverimport.Work,
	now int64,
	access serverimport.WorkerAccess,
) (map[string]int64, error) {
	if err := records.Lock(ctx, unit, now, access); err != nil {
		return nil, fmt.Errorf("lock import outcome: %w", err)
	}
	counts, err := records.Counts(ctx, unit)
	if err != nil {
		return nil, fmt.Errorf("read import outcome counts: %w", err)
	}
	return counts, nil
}

func movePending(counts map[string]int64, state string) map[string]int64 {
	result := maps.Clone(counts)
	result[state] += result["PENDING"] + result["EVALUATING"]
	delete(result, "PENDING")
	delete(result, "EVALUATING")
	return result
}

func terminalCounts(counts map[string]int64) serverimport.TerminalCounts {
	return serverimport.TerminalCounts{
		Matched:          counts["IMPORTED_MATCHED"],
		Warning:          counts["IMPORTED_WARNING"],
		Missing:          counts["IMPORTED_MISSING_ENTRY"],
		NotFound:         counts["NOT_FOUND"],
		SkippedExisting:  counts["SKIPPED_EXISTING"],
		SkippedNotBetter: counts["SKIPPED_NOT_BETTER"],
		SameBytes:        counts["ALREADY_SAME_BYTES"],
		Failed: counts["SOURCE_CHANGED"] + counts["CATALOG_CHANGED"] +
			counts["READ_FAILED"] + counts["INVALID_ARCHIVE"] +
			counts["COMMIT_FAILED"],
		Cancelled: counts["CANCELLED"],
	}
}

func retryExecution(
	ctx context.Context,
	records outcomeRecords,
	unit serverimport.Work,
	counts map[string]int64,
	code string,
	now int64,
) (int64, error) {
	budget, err := records.Budget(ctx, unit)
	if err != nil {
		return 0, fmt.Errorf("read import retry budget: %w", err)
	}
	var terminal int64
	for state, count := range counts {
		if state != "PENDING" && state != "EVALUATING" {
			terminal += count
		}
	}
	at, retry := serverimport.AutomaticRetryAt(
		budget.Attempt, budget.Maximum, terminal, budget.Deadline, now,
	)
	if !retry {
		return 0, nil
	}
	event, err := json.Marshal(map[string]any{
		"schemaVersion": 1,
		"attempt":       budget.Attempt,
		"retryAtMs":     at,
		"errorCode":     code,
	})
	if err != nil {
		return 0, fmt.Errorf("encode automatic retry: %w", err)
	}
	cmd := serverimport.AutomaticRetry{
		Unit: unit, AvailableAt: at, Now: now, Event: event,
	}
	if err := records.Retry(ctx, cmd); err != nil {
		return 0, fmt.Errorf("write automatic retry: %w", err)
	}
	return at, nil
}
