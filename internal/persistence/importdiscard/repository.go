package importdiscard

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/service/importdiscard"
)

type (
	Repository struct{ database dbapi.DB }
	records    struct{ executor dbapi.Executor }
	writes     struct{ transaction dbapi.Tx }
)

func New(database dbapi.DB) *Repository { return &Repository{database} }
func (repository *Repository) WithRead(ctx context.Context, work func(importdiscard.Reader) error) error {
	tx, err := repository.database.BeginTx(ctx, &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin discard read: %w", err)
	}
	defer dbapi.Rollback(tx)
	if err := work(records{tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit discard read: %w", err)
	}
	return nil
}

func (repository *Repository) WithWrite(ctx context.Context, work func(importdiscard.WriteScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin discard write: %w", err)
	}
	defer dbapi.Rollback(tx)
	bound := writes{tx}
	if err := work(
		importdiscard.WriteScope{
			Reader: records{
				tx,
			},
			Requests: bound,
			Sources:  bound,
		},
	); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit discard write: %w", err)
	}
	return nil
}

func batchTable(kind string) (string, error) {
	switch kind {
	case "IMPORT":
		return "import_jobs", nil
	case "SOURCE":
		return "source_imports", nil

	default:
		return "", importdiscard.ErrInvalid
	}
}
