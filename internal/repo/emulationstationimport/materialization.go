package emulationstationimport

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
)

type Materialization struct {
	database      *sql.DB
	preCommitHook func(dbexec.Executor) error
}

func NewMaterialization(database *sql.DB) *Materialization {
	return &Materialization{database: database}
}

func (repository *Materialization) WithPreCommitHook(
	hook func(dbexec.Executor) error,
) {
	repository.preCommitHook = hook
}

func (repository *Materialization) LoadMaterialSource(
	ctx context.Context, key application.MaterialKey,
) (application.MaterialSnapshot, error) {
	return materialRecords{
		executor: repository.database,
	}.Source(ctx, key)
}

func (repository *Materialization) LoadExecutionPhase(
	ctx context.Context, id string,
) (application.ExecutionPhase, error) {
	return materialRecords{
		executor: repository.database,
	}.Execution(ctx, id)
}

func (repository *Materialization) CommitMaterialBinding(
	ctx context.Context, change application.MaterialBinding,
) (string, error) {
	return commitResultTx(ctx, repository.database, repository.preCommitHook,
		"EmulationStation material binding",
		func(tx *sql.Tx) (string, error) {
			return materialRecords{transaction: tx, executor: tx}.Bind(ctx, change)
		},
	)
}

func (repository *Materialization) CommitMaterialWarning(
	ctx context.Context, change application.MaterialWarning,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin EmulationStation material warning: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := materialRecords{transaction: tx, executor: tx}
	if err := records.Warn(ctx, change); err != nil {
		return err
	}
	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(tx); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit EmulationStation material warning: %w", err)
	}
	return nil
}

func (repository *Materialization) CommitPhaseChange(
	ctx context.Context, change application.PhaseChange,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin EmulationStation phase change: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := materialRecords{transaction: tx, executor: tx}
	if err := records.Phase(ctx, change); err != nil {
		return err
	}
	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(tx); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit EmulationStation phase change: %w", err)
	}
	return nil
}

type materialRecords struct {
	transaction *sql.Tx
	executor    dbexec.Executor
}

func (records materialRecords) Execution(
	ctx context.Context, id string,
) (application.ExecutionPhase, error) {
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
