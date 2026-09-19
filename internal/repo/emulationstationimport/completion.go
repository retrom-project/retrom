package emulationstationimport

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
)

type Completion struct{ database *sql.DB }

func NewCompletion(database *sql.DB) *Completion { return &Completion{database: database} }
func (repository *Completion) CommitCompletion(ctx context.Context, unit application.Execution, nowMS int64) error {
	return dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := completionRecords{executor: executor}
		before, found, err := records.Current(ctx, unit.JobID)
		if err != nil {
			return fmt.Errorf("read current EmulationStation execution: %w", err)
		}
		if !found || before.Execution != unit {
			return application.ErrVersionConflict
		}
		if before.Kind != "SERVER_EMULATIONSTATION_IMPORT" || application.ExecutionState(before, unit, nowMS) != application.LeaseActive {
			return application.ErrVersionConflict
		}
		counts, err := records.Counts(ctx, unit.ImportID)
		if err != nil {
			return fmt.Errorf("read EmulationStation completion counts: %w", err)
		}
		if counts.Unfinished > 0 {
			return application.ErrActive
		}
		change, err := application.PlanCompletion(before, counts, nowMS)
		if err != nil {
			return err
		}
		if err := records.Complete(ctx, change); err != nil {
			return fmt.Errorf("persist EmulationStation completion: %w", err)
		}
		return scheduleTerminalPayloads(ctx, executor, change.Before.ImportID, change.NowMS)
	})
}

type completionRecords struct {
	transaction *sql.Tx
	executor    dbexec.Executor
}

func (records completionRecords) Current(ctx context.Context, id string) (application.LeaseSnapshot, bool, error) {
	return executionRecords(records).Current(ctx, id)
}

func (records completionRecords) Counts(ctx context.Context, id string) (application.CompletionCounts, error) {
	var counts application.CompletionCounts
	var err error
	counts.Terminal, err = LoadTerminalItemCounts(ctx, records.executor, id)
	if err != nil {
		return application.CompletionCounts{}, err
	}
	err = records.executor.QueryRowContext(ctx, `SELECT game_count,
(SELECT count(*) FROM emulationstation_import_items WHERE import_id=plan.id AND execution_state IN
('PENDING','COPYING','VALIDATING')),
(SELECT count(*) FROM emulationstation_import_items WHERE import_id=plan.id AND retryable=1 AND
execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')),
(SELECT count(*) FROM emulationstation_import_items item,json_each(item.warnings_json) warning WHERE
item.import_id=plan.id AND json_extract(warning.value,'$.pathKind') IN ('COVER','VIDEO'))
FROM emulationstation_imports plan WHERE id=?`, id).Scan(
		&counts.ExpectedItems,
		&counts.Unfinished,
		&counts.RetryableFailed,
		&counts.MediaWarnings,
	)
	if err != nil {
		return application.CompletionCounts{}, fmt.Errorf("read EmulationStation completion projection: %w", err)
	}
	return counts, nil
}
