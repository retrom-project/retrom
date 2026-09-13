package platforminstance

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/persistence/dbexec"
	"retrom/internal/service/platforminstance"
)

type (
	Repository struct{ database *sql.DB }
	records    struct{ database dbexec.Executor }
)

func New(database *sql.DB) *Repository { return &Repository{database: database} }

func (repository *Repository) WithRead(ctx context.Context, work func(platforminstance.Reader) error) error {
	transaction, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
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
	connection, err := repository.database.Conn(ctx)
	if err != nil {
		return fmt.Errorf("platforminstance: acquire connection: %w", err)
	}
	defer func() { _ = connection.Close() }()
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("platforminstance: begin immediate: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = connection.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		}
	}()
	bound := records{connection}
	if err := work(platforminstance.WriteScope{Reader: bound, Directories: bound, Idempotency: bound}); err != nil {
		return err
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("platforminstance: commit: %w", err)
	}
	committed = true
	return nil
}
