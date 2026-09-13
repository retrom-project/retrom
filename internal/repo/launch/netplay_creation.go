package launch

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/repo/dbexec"
	application "retrom/internal/service/launch"
)

type NetplayCreation struct{ database *sql.DB }

func NewNetplayCreation(database *sql.DB) *NetplayCreation {
	return &NetplayCreation{database: database}
}

type netplayCreationRecords struct {
	executor    dbexec.Executor
	transaction *sql.Tx
}

func (repository *NetplayCreation) Snapshot(
	ctx context.Context,
	request application.NetplayCreateRequest,
) (application.NetplayCreationSnapshot, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.NetplayCreationSnapshot{}, fmt.Errorf("begin netplay snapshot: %w", err)
	}
	defer dbexec.Rollback(tx)
	snapshot, err := (netplayCreationRecords{executor: tx}).Snapshot(ctx, request)
	if err != nil {
		return application.NetplayCreationSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.NetplayCreationSnapshot{}, fmt.Errorf("commit netplay snapshot: %w", err)
	}
	return snapshot, nil
}

func (repository *NetplayCreation) WithCreation(
	ctx context.Context,
	work func(application.NetplayCreationScope) error,
) error {
	// The shared store serializes writers before this final participant lookup.
	// Another adapter must preserve that atomic lookup/create/bind guarantee.
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin netplay creation: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := work(netplayCreationRecords{executor: tx, transaction: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit netplay creation: %w", err)
	}
	return nil
}
