package pegasusimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/repo/dbexec"
	application "retrom/internal/service/pegasusimport"
)

type Materialization struct{ database *sql.DB }

func NewMaterialization(database *sql.DB) *Materialization {
	return &Materialization{database: database}
}

func (repository *Materialization) WithMaterialization(
	ctx context.Context,
	work func(application.MaterialScope) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Pegasus material transaction: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := materialRecords{tx: tx}
	if err := work(application.MaterialScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Pegasus material transaction: %w", err)
	}
	return nil
}

type materialRecords struct{ tx *sql.Tx }

func (records materialRecords) Execution(ctx context.Context, id string) (application.ExecutionPhase, error) {
	before, err := leaseRecords(records).Current(ctx, id)
	if err != nil {
		return application.ExecutionPhase{}, err
	}
	result := application.ExecutionPhase{Execution: before}
	if err := records.tx.QueryRowContext(ctx,
		`SELECT COALESCE(phase,'') FROM pegasus_imports WHERE id=?`, before.ImportID).Scan(
		&result.Phase,
	); err != nil {
		return application.ExecutionPhase{}, fmt.Errorf("read Pegasus phase: %w", err)
	}
	return result, nil
}
