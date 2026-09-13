package emulationstationimport

import (
	"context"
	"database/sql"
	"fmt"
	payload "retrom/internal/persistence/payloadrelease"

	"retrom/internal/dbexec"
	application "retrom/internal/service/emulationstationimport"
)

type Completion struct{ database *sql.DB }

func NewCompletion(database *sql.DB) *Completion { return &Completion{database: database} }
func (repository *Completion) WithCompletion(ctx context.Context, run func(application.CompletionScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin EmulationStation completion: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := completionRecords{transaction: tx, executor: tx}
	if err := run(application.CompletionScope{Payload: payload.BindReleases(tx), Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit EmulationStation completion: %w", err)
	}
	return nil
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
