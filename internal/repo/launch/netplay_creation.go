package launch

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/launch"
	"retrom/internal/repo/dbexec"
)

type NetplayCreation struct{ database *sql.DB }

func NewNetplayCreation(database *sql.DB) *NetplayCreation {
	return &NetplayCreation{database: database}
}

type netplayCreationRecords struct {
	executor    dbexec.Executor
	transaction *sql.Tx
}

func (repository *NetplayCreation) LoadNetplaySnapshot(
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

func (repository *NetplayCreation) CommitNetplayCreation(
	ctx context.Context,
	plan application.NetplayCreationPlan,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin netplay creation: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := netplayCreationRecords{executor: tx, transaction: tx}
	if err := records.Create(ctx, plan); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit netplay creation: %w", err)
	}
	return nil
}
