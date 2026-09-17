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
func (repository *Repository) Batch(ctx context.Context, key importdiscard.Key) (importdiscard.Batch, error) {
	return records{repository.database}.Batch(ctx, key)
}

func (repository *Repository) Disposition(
	ctx context.Context, key importdiscard.Key,
) (importdiscard.Disposition, bool, error) {
	return records{repository.database}.Disposition(ctx, key)
}

func (repository *Repository) Pending(ctx context.Context) (importdiscard.Request, bool, error) {
	return records{repository.database}.Pending(ctx)
}

func (repository *Repository) Children(ctx context.Context, key importdiscard.Key) ([]string, error) {
	return records{repository.database}.Children(ctx, key)
}

func (repository *Repository) writeScope(tx *sql.Tx) importdiscard.WriteScope {
	bound := writes{tx}
	return importdiscard.WriteScope{
		Reader: records{tx}, Requests: bound, Sources: bound, Ownership: bound,
	}
}

func (repository *Repository) CommitRecoverOwnership(
	ctx context.Context,
	cmd importdiscard.RecoverOwnershipCommand,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin discard write: %w", err)
	}
	defer dbexec.Rollback(tx)
	scope := repository.writeScope(tx)
	if err := recoverOwnership(ctx, scope, cmd.Key); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit discard write: %w", err)
	}
	return nil
}

func (repository *Repository) CommitDiscardSourceItems(
	ctx context.Context,
	cmd importdiscard.DiscardSourceItemsCommand,
) (bool, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin discard write: %w", err)
	}
	defer dbexec.Rollback(tx)
	scope := repository.writeScope(tx)
	done, err := discardSourceItems(ctx, scope, cmd.Key, cmd.NowMS)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit discard write: %w", err)
	}
	return done, nil
}

func (repository *Repository) CommitRequestDiscard(
	ctx context.Context,
	cmd importdiscard.RequestDiscardCommand,
) (importdiscard.Status, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return importdiscard.Status{}, fmt.Errorf("begin discard write: %w", err)
	}
	defer dbexec.Rollback(tx)
	scope := repository.writeScope(tx)
	result, err := requestDiscard(ctx, scope, cmd)
	if err != nil {
		return importdiscard.Status{}, err
	}
	if err := tx.Commit(); err != nil {
		return importdiscard.Status{}, fmt.Errorf("commit discard write: %w", err)
	}
	return result, nil
}

func (repository *Repository) CommitProgress(ctx context.Context, progress importdiscard.Progress) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin discard write: %w", err)
	}
	defer dbexec.Rollback(tx)
	bound := writes{tx}
	if err := bound.Progress(ctx, progress); err != nil {
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
