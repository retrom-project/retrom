package sourceimport

import (
	"context"
	"fmt"

	payload "retrom/internal/persistence/sourceimport/sourcerelease"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/sourceimport"
)

type ItemWork struct{ database dbapi.DB }

func NewItemWork(database dbapi.DB) *ItemWork { return &ItemWork{database: database} }
func (repository *ItemWork) WithItemWork(ctx context.Context, work func(application.ItemWorkScope) error) error {
	err := dbapi.RetryTransaction(ctx, repository.database, func(tx dbapi.Tx) error {
		records := itemWorkRecords{tx}
		if err := work(application.ItemWorkScope{
			Payload: payload.BindReleases(tx), Read: records, Write: records,
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit Source item work: %w", err)
	}
	return nil
}

type itemWorkRecords struct{ tx dbapi.Tx }

func (records itemWorkRecords) Execution(ctx context.Context, id string) (application.ExecutionSnapshot, error) {
	return scanRecovery(dbapi.QueryRowContext(ctx, records.tx, recoverySnapshotSQL+` AND job.id=?`, id))
}

const itemExecutionFence = ` AND EXISTS(SELECT 1 FROM source_imports plan JOIN jobs job ON job.id=plan.import_job_id
WHERE plan.id=? AND plan.version=? AND plan.state=? AND job.id=? AND job.scope_type='SOURCE_IMPORT'
AND job.scope_id=plan.id AND job.kind='IMPORT_RECEIVE' AND job.version=? AND job.state=?
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
