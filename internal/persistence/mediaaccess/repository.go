package mediaaccess

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/persistence/dbexec"
	service "retrom/internal/service/mediaaccess"
)

type (
	Repository struct{ database *sql.DB }
	reader     struct{ executor dbexec.Executor }
)

func New(database *sql.DB) *Repository { return &Repository{database: database} }

func (repository *Repository) WithRead(ctx context.Context, work func(service.Reader) error) error {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin media access snapshot: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := work(reader{executor: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit media access snapshot: %w", err)
	}
	return nil
}
