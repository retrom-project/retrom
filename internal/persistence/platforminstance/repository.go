package platforminstance

import (
	"context"
	"fmt"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/dberrors"
	"retrom/internal/service/platforminstance"
)

type (
	Repository struct{ database dbapi.DB }
	records    struct{ database dbapi.Executor }
)

func New(database dbapi.DB) *Repository { return &Repository{database: database} }

func (repository *Repository) WithRead(ctx context.Context, work func(platforminstance.Reader) error) error {
	transaction, err := repository.database.BeginTx(ctx, &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("platforminstance: begin read: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	if err := work(records{transaction}); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("platforminstance: commit read: %w", err)
	}
	return nil
}

func (repository *Repository) WithWrite(ctx context.Context, work func(platforminstance.WriteScope) error) error {
	write := func(tx dbapi.Tx) error {
		bound := records{tx}
		return work(platforminstance.WriteScope{Reader: bound, Directories: bound, Idempotency: bound})
	}
	var err error
	for attempt := range 8 {
		err = dbapi.RetryTransaction(ctx, repository.database, write)
		if err == nil || attempt == 7 || (!dberrors.Unique(err, "platform_instances_platform_id_slug_key") &&
			!dberrors.Unique(err, "idempotency_records_pkey")) {
			break
		}
		// Each concurrent winner reserves another suffix. A fresh snapshot must
		// recompute the slug (or replay its receipt) after every collision.
		timer := time.NewTimer(time.Duration(min(1<<attempt, 4)) * 5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("retry platform directory creation: %w", ctx.Err())
		case <-timer.C:
		}
	}
	if err != nil {
		return fmt.Errorf("commit platforminstance transaction: %w", err)
	}
	return nil
}
