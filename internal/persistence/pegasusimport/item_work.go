package pegasusimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/dbexec"
	application "retrom/internal/service/pegasusimport"
)

type ItemWork struct{ database *sql.DB }

func NewItemWork(database *sql.DB) *ItemWork { return &ItemWork{database: database} }
func (repository *ItemWork) WithItemWork(ctx context.Context, work func(application.ItemWorkScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Pegasus item work: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := itemWorkRecords{tx}
	if err := work(application.ItemWorkScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Pegasus item work: %w", err)
	}
	return nil
}

type itemWorkRecords struct{ tx *sql.Tx }

func (records itemWorkRecords) Execution(ctx context.Context, id string) (application.ExecutionSnapshot, error) {
	return scanRecovery(records.tx.QueryRowContext(ctx, recoverySnapshotSQL+` AND job.id=?`, id))
}

const itemExecutionFence = ` AND EXISTS(SELECT 1 FROM pegasus_imports plan JOIN jobs job ON job.id=plan.import_job_id
WHERE plan.id=? AND plan.version=? AND plan.state=? AND job.id=? AND job.scope_type='PEGASUS_IMPORT'
AND job.scope_id=plan.id AND job.kind='SERVER_PEGASUS_IMPORT' AND job.version=? AND job.state=?
AND job.worker_id=? AND job.execution_no=? AND job.attempt_count=? AND job.leased_until_ms=? AND job.leased_until_ms>?
AND job.execution_deadline_at_ms=? AND job.execution_deadline_at_ms>?)`

func itemFenceArgs(before application.OwnedItem, now int64) []any {
	execution := before.Execution
	return []any{
		before.Item.ID, before.Item.ImportID, before.Item.Version, before.Item.State,
		execution.ImportID, execution.ImportVersion, execution.ImportState, execution.JobID, execution.JobVersion,
		execution.JobState, execution.WorkerID, execution.ExecutionNo, execution.Attempt,
		execution.LeaseUntilMS, now, execution.DeadlineMS, now,
	}
}
