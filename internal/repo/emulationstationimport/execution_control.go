package emulationstationimport

import (
	"context"
	"database/sql"
	"fmt"

	payload "retrom/internal/repo/payloadrelease"

	application "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
	library "retrom/internal/repo/libraryimport"
)

type ExecutionControl struct{ database *sql.DB }

func NewExecutionControl(database *sql.DB) *ExecutionControl {
	return &ExecutionControl{database: database}
}

func (repository *ExecutionControl) WithExecution(
	ctx context.Context,
	run func(application.ExecutionScope) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin EmulationStation execution control: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := executionRecords{transaction: tx, executor: tx}
	scope := application.ExecutionScope{
		Payload: payload.BindReleases(tx), Read: records, Write: records,
		Metadata: library.BindMetadata(tx),
	}
	if err := run(scope); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit EmulationStation execution control: %w", err)
	}
	return nil
}

type executionRecords struct {
	transaction *sql.Tx
	executor    dbexec.Executor
}

func (records executionRecords) Current(ctx context.Context, id string) (application.LeaseSnapshot, bool, error) {
	return (leaseRecords{executor: records.executor}).Current(ctx, id)
}

func (records executionRecords) TerminalCount(ctx context.Context, id string) (int64, error) {
	var count int64
	err := records.executor.QueryRowContext(ctx, `SELECT count(*) FROM emulationstation_import_items
WHERE import_id=? AND execution_state NOT IN ('PENDING','COPYING','VALIDATING')`, id).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count terminal EmulationStation execution items: %w", err)
	}
	return count, nil
}

func (records executionRecords) fence(ctx context.Context, change application.ExecutionFinish) error {
	before := change.Before
	result, err := records.executor.ExecContext(ctx, `UPDATE jobs SET version=version WHERE id=? AND version=?
AND state=? AND kind=? AND scope_type='EMULATIONSTATION_IMPORT' AND scope_id=?
AND execution_no=? AND attempt_count=? AND max_attempts=? AND worker_id=?
AND leased_until_ms=? AND leased_until_ms>? AND execution_started_at_ms IS ?
AND execution_deadline_at_ms=? AND execution_deadline_at_ms>?
AND EXISTS(SELECT 1 FROM emulationstation_imports plan WHERE plan.id=? AND plan.version=? AND plan.state=?
AND plan.root_id=? AND plan.root_config_digest=? AND plan.source_relative_path=?
AND plan.created_by_user_id=? AND plan.release_year_max=?
AND ((jobs.kind='SERVER_EMULATIONSTATION_SCAN' AND plan.scan_job_id=jobs.id AND plan.import_job_id IS NULL)
OR (jobs.kind='SERVER_EMULATIONSTATION_IMPORT' AND plan.import_job_id=jobs.id)))`,
		before.JobID, before.JobVersion, before.JobState, before.Kind, before.ImportID, before.ExecutionNo, before.Attempt,
		before.MaxAttempts, before.WorkerID, before.LeaseUntilMS, change.NowMS, before.StartedAtMS,
		before.DeadlineAtMS, change.NowMS,
		before.ImportID, before.ImportVersion, before.ImportState, before.RootID, before.RootDigest, before.RelativePath,
		before.CreatedByUserID, before.ReleaseYearMax)
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}
