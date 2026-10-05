package database

import (
	"context"
	"fmt"
)

type lifecycleKey struct{}

// TransactionLifecycle binds an application participant to a short write unit.
// End is called only after the underlying commit/rollback has returned.
type TransactionLifecycle interface {
	Begin(context.Context, Tx) func(bool)
}

func WithTransactionLifecycle(ctx context.Context, lifecycle TransactionLifecycle) context.Context {
	return context.WithValue(ctx, lifecycleKey{}, lifecycle)
}

// ObserveTransaction leaves read snapshots and unobserved transactions alone.
func ObserveTransaction(ctx context.Context, tx Tx, options *TxOptions) Tx {
	lifecycle, ok := ctx.Value(lifecycleKey{}).(TransactionLifecycle)
	if !ok || options != nil && options.ReadOnly {
		return tx
	}
	end := lifecycle.Begin(ctx, tx)
	return &observedTransaction{Tx: tx, finish: end}
}

type observedTransaction struct {
	Tx
	finish func(bool)
	ended  bool
}

func (tx *observedTransaction) Commit() error {
	err := tx.Tx.Commit()
	tx.end(err == nil)
	if err != nil {
		return fmt.Errorf("commit observed transaction: %w", err)
	}
	return nil
}

func (tx *observedTransaction) Rollback() error {
	err := tx.Tx.Rollback()
	tx.end(false)
	if err != nil {
		return fmt.Errorf("rollback observed transaction: %w", err)
	}
	return nil
}

func (tx *observedTransaction) end(committed bool) {
	if !tx.ended {
		tx.ended = true
		tx.finish(committed)
	}
}
