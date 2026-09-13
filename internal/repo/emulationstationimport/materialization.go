package emulationstationimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/repo/dbexec"
	application "retrom/internal/service/emulationstationimport"
)

type Materialization struct{ database *sql.DB }

func NewMaterialization(database *sql.DB) *Materialization {
	return &Materialization{database: database}
}

func (repository *Materialization) WithMaterialization(
	ctx context.Context,
	run func(application.MaterialScope) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin EmulationStation material transaction: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := materialRecords{transaction: tx, executor: tx}
	if err := run(application.MaterialScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit EmulationStation material transaction: %w", err)
	}
	return nil
}

type materialRecords struct {
	transaction *sql.Tx
	executor    dbexec.Executor
}

func (records materialRecords) Execution(ctx context.Context, id string) (application.ExecutionPhase, error) {
	execution, found, err := executionRecords(records).Current(ctx, id)
	if err != nil {
		return application.ExecutionPhase{}, err
	}
	if !found {
		return application.ExecutionPhase{}, application.ErrVersionConflict
	}
	result := application.ExecutionPhase{Execution: execution}
	if err := records.executor.QueryRowContext(
		ctx, `SELECT COALESCE(phase,'') FROM emulationstation_imports WHERE id=?`,
		execution.ImportID,
	).Scan(

		&result.Phase,
	); err != nil {
		return application.ExecutionPhase{}, fmt.Errorf("read EmulationStation phase: %w", err)
	}
	return result, nil
}
