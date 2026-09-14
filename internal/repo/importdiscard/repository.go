package importdiscard

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/model/importdiscard"
	"retrom/internal/repo/dbexec"
)

type (
	Repository struct{ database *sql.DB }
	records    struct{ executor dbexec.Executor }
	writes     struct{ transaction *sql.Tx }
)

func New(database *sql.DB) *Repository { return &Repository{database} }
func (repository *Repository) WithRead(ctx context.Context, work func(importdiscard.Reader) error) error {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin discard read: %w", err)
	}
	defer dbexec.Rollback(tx)
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
	defer dbexec.Rollback(tx)
	bound := writes{tx}
	if err := work(
		importdiscard.WriteScope{
			Reader: records{
				tx,
			},
			Requests:  bound,
			Sources:   bound,
			Ownership: bound,
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
	case "PEGASUS":
		return "pegasus_imports", nil
	case "EMULATIONSTATION":
		return "emulationstation_imports", nil
	default:
		return "", importdiscard.ErrInvalid
	}
}
