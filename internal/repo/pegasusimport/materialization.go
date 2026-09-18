package pegasusimport

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/pegasusimport"
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
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.MaterialSnapshot{}, fmt.Errorf(
			"begin Pegasus material source read: %w", err,
		)
	}
	defer dbexec.Rollback(tx)
	result, err := materialRecords{tx: tx, executor: tx}.Source(ctx, key)
	if err != nil {
		return application.MaterialSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.MaterialSnapshot{}, fmt.Errorf(
			"commit Pegasus material source read: %w", err,
		)
	}
	return result, nil
}

func (repository *Materialization) LoadExecutionPhase(
	ctx context.Context, id string,
) (application.ExecutionPhase, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ExecutionPhase{}, fmt.Errorf(
			"begin Pegasus execution phase read: %w", err,
		)
	}
	defer dbexec.Rollback(tx)
	result, err := materialRecords{tx: tx, executor: tx}.Execution(ctx, id)
	if err != nil {
		return application.ExecutionPhase{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.ExecutionPhase{}, fmt.Errorf(
			"commit Pegasus execution phase read: %w", err,
		)
	}
	return result, nil
}

func (repository *Materialization) CommitMaterialBinding(
	ctx context.Context, change application.MaterialBinding,
) (string, error) {
	return commitResultTx(ctx, repository.database, repository.preCommitHook,
		"Pegasus material binding",
		func(tx *sql.Tx) (string, error) {
			return materialRecords{tx: tx, executor: tx}.Bind(ctx, change)
		},
	)
}

func (repository *Materialization) CommitMaterialWarning(
	ctx context.Context, change application.MaterialWarning,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Pegasus material warning: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := materialRecords{tx: tx, executor: tx}
	if err := records.Warn(ctx, change); err != nil {
		return err
	}
	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(tx); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Pegasus material warning: %w", err)
	}
	return nil
}

func (repository *Materialization) CommitPhaseChange(
	ctx context.Context, change application.PhaseChange,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Pegasus phase change: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := materialRecords{tx: tx, executor: tx}
	if err := records.Phase(ctx, change); err != nil {
		return err
	}
	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(tx); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Pegasus phase change: %w", err)
	}
	return nil
}

type materialRecords struct {
	tx       *sql.Tx
	executor dbexec.Executor
}

func (records materialRecords) Execution(
	ctx context.Context, id string,
) (application.ExecutionPhase, error) {
	before, err := leaseRecords{tx: records.tx}.Current(ctx, id)
	if err != nil {
		return application.ExecutionPhase{}, err
	}
	result := application.ExecutionPhase{Execution: before}
	if err := records.executor.QueryRowContext(ctx,
		`SELECT COALESCE(phase,'') FROM pegasus_imports WHERE id=?`, before.ImportID).Scan(
		&result.Phase,
	); err != nil {
		return application.ExecutionPhase{}, fmt.Errorf("read Pegasus phase: %w", err)
	}
	return result, nil
}
