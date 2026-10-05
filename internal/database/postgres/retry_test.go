package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	dbapi "retrom/internal/database"
)

func TestRetryTransactionRechecksAfterConcurrentCommit(t *testing.T) {
	first, second := transactionDatabases(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	read := make(chan struct{}, 2)
	release := make(chan struct{})
	results := make(chan error, 2)
	for _, db := range []dbapi.DB{first, second} {
		go func() {
			attempt := 0
			results <- dbapi.RetryTransaction(ctx, db, func(tx dbapi.Tx) error {
				attempt++
				var value int
				if err := dbapi.QueryRowContext(ctx, tx, "SELECT value FROM items").Scan(&value); err != nil {
					return err
				}
				if attempt == 1 {
					read <- struct{}{}
					select {
					case <-release:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				_, err := tx.ExecContext(ctx, "UPDATE items SET value=?", value+1)
				return err
			})
		}()
	}
	for range 2 {
		select {
		case <-read:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	requireItemSum(ctx, t, first, 3)
}

func TestRetryTransactionDoesNotReplayApplicationFailure(t *testing.T) {
	db, _ := transactionDatabases(t)
	attempts := 0
	err := dbapi.RetryTransaction(t.Context(), db, func(tx dbapi.Tx) error {
		attempts++
		if _, err := tx.ExecContext(t.Context(), "UPDATE items SET value=2"); err != nil {
			return err
		}
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("attempts=%d error=%v", attempts, err)
	}
	requireItemSum(t.Context(), t, db, 1)
}

func TestRetryTransactionSurvivesConsecutiveConcurrentCommits(t *testing.T) {
	first, second := transactionDatabases(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	attempts := 0
	err := dbapi.RetryTransaction(ctx, first, func(tx dbapi.Tx) error {
		attempts++
		var value int
		if err := dbapi.QueryRowContext(ctx, tx, "SELECT value FROM items").Scan(&value); err != nil {
			return err
		}
		if attempts <= 4 {
			if _, err := second.ExecContext(ctx, "UPDATE items SET value=value+1"); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, "UPDATE items SET value=?", value+1)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 5 {
		t.Fatalf("attempts=%d, want one successful write after four concurrent commits", attempts)
	}
	requireItemSum(ctx, t, first, 6)
}
