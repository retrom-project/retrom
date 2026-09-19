package pegasusimport

import (
	"context"
	"database/sql"
	"fmt"

	payloadmodel "retrom/internal/model/payloadrelease"
	payloadrepo "retrom/internal/repo/payloadrelease"

	application "retrom/internal/model/pegasusimport"
	"retrom/internal/repo/dbexec"
)

type ItemWork struct{ database *sql.DB }

func NewItemWork(database *sql.DB) *ItemWork { return &ItemWork{database: database} }

func (repository *ItemWork) ClaimNextItem(ctx context.Context, unit application.ExecutionIdentity, nowMS int64) (application.ClaimNextItemResult, error) {
	var result application.ClaimNextItemResult
	err := dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := itemWorkRecords{executor}
		execution, err := records.Execution(ctx, unit.JobID)
		if err != nil {
			return fmt.Errorf("read Pegasus item execution: %w", err)
		}
		if err := application.ValidateExecution(execution, unit, nowMS); err != nil {
			return err
		}
		if execution.Kind != "SERVER_PEGASUS_IMPORT" || execution.JobState != "RUNNING" {
			return application.ErrVersionConflict
		}
		item, found, err := records.Next(ctx, unit.ImportID)
		if err != nil {
			return fmt.Errorf("read next Pegasus item: %w", err)
		}
		if !found {
			return nil
		}
		result.Found = true
		if item.ImportID != unit.ImportID || item.State != "PENDING" || !application.ValidItemVersion(item.Version) {
			return application.ErrVersionConflict
		}
		if err := records.Claim(ctx, application.ItemClaim{
			Before: application.OwnedItem{Execution: execution, Item: item}, NowMS: nowMS,
		}); err != nil {
			return fmt.Errorf("claim Pegasus item: %w", err)
		}
		item.State, item.Version = "COPYING", item.Version+1
		result.Item = item
		return nil
	})
	if err != nil {
		return application.ClaimNextItemResult{}, err
	}
	return result, nil
}

func (repository *ItemWork) CommitItemResume(ctx context.Context, unit application.ExecutionIdentity, itemID, jobID, ordinaryID string, nowMS int64) error {
	return dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := itemWorkRecords{executor}
		before, err := records.Current(ctx, itemID)
		if err != nil {
			return fmt.Errorf("read Pegasus item ownership: %w", err)
		}
		if err := application.ValidateExecution(before.Execution, unit, nowMS); err != nil {
			return err
		}
		if before.Item.ID != itemID || before.Item.ImportID != unit.ImportID || !application.ValidItemVersion(before.Item.Version) {
			return application.ErrVersionConflict
		}
		if jobID == "" || ordinaryID == "" || before.Item.LibraryImportJobID != jobID ||
			before.Item.LibraryImportItemID != ordinaryID {
			return application.ErrVersionConflict
		}
		if before.Item.State == "VALIDATING" || before.Item.State == "REVIEW_PENDING" {
			return nil
		}
		if before.Item.State != "COPYING" {
			return application.ErrVersionConflict
		}
		if err := records.Resume(ctx, application.ItemResume{Before: before, NowMS: nowMS}); err != nil {
			return fmt.Errorf("resume Pegasus review item: %w", err)
		}
		return nil
	})
}

func (repository *ItemWork) CommitItemFinish(ctx context.Context, unit application.ExecutionIdentity, itemID string, outcome application.ItemOutcome, nowMS int64) error {
	return dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := itemWorkRecords{executor}
		before, err := records.Current(ctx, itemID)
		if err != nil {
			return fmt.Errorf("read Pegasus item ownership: %w", err)
		}
		if err := application.ValidateExecution(before.Execution, unit, nowMS); err != nil {
			return err
		}
		if before.Item.ID != itemID || before.Item.ImportID != unit.ImportID || !application.ValidItemVersion(before.Item.Version) {
			return application.ErrVersionConflict
		}
		if before.Item.State == outcome.State {
			return nil
		}
		if before.Item.State != "COPYING" && before.Item.State != "VALIDATING" {
			return application.ErrVersionConflict
		}
		if err := records.Finish(ctx, application.ItemFinish{Before: before, Outcome: outcome, NowMS: nowMS}); err != nil {
			return fmt.Errorf("save Pegasus item outcome: %w", err)
		}
		scope := payloadrepo.BindReleases(executor)
		_, err = payloadrepo.NewScheduler(nil).TerminalSource(
			ctx, scope.Scheduling,
			payloadmodel.Scope{Type: payloadmodel.ScopePegasusImportItem, ID: itemID}, nowMS,
		)
		if err != nil {
			return fmt.Errorf("schedule Pegasus item payloads: %w", err)
		}
		return nil
	})
}

type itemWorkRecords struct{ tx dbexec.Executor }

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
