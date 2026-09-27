package mediaaccess

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	service "retrom/internal/service/mediaaccess"
)

type (
	Repository struct{ database dbapi.DB }
	reader     struct{ executor dbapi.Executor }
)

func New(database dbapi.DB) *Repository { return &Repository{database: database} }

func (repository *Repository) WithRead(ctx context.Context, work func(service.Reader) error) error {
	tx, err := repository.database.BeginTx(ctx, &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin media access snapshot: %w", err)
	}
	defer dbapi.Rollback(tx)
	if err := work(reader{executor: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit media access snapshot: %w", err)
	}
	return nil
}
