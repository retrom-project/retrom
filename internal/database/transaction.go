package database

import (
	"context"
	"fmt"
)

// InTransaction owns commit and rollback while callers use a transaction interface.
func InTransaction(ctx context.Context, db DB, options *TxOptions, work func(Tx) error) error {
	tx, err := db.BeginTx(ctx, options)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer Rollback(tx)
	if err := work(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// InImmediateTransaction obtains SQLite's write reservation before running work.
func InImmediateTransaction(ctx context.Context, db DB, work func(Tx) error) error {
	tx, err := db.BeginImmediate(ctx)
	if err != nil {
		return fmt.Errorf("begin immediate transaction: %w", err)
	}
	defer Rollback(tx)
	if err := work(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit immediate transaction: %w", err)
	}
	return nil
}
