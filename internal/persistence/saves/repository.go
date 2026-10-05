package saves

import (
	"context"
	"database/sql"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/service/saves"
)

type (
	Repository struct{ database dbapi.DB }
	records    struct{ executor dbapi.Executor }
	writes     struct {
		records
		transaction dbapi.Tx
	}
)

func New(database dbapi.DB) *Repository { return &Repository{database: database} }
func (repository *Repository) LoadLaunch(ctx context.Context, id string) (saves.Launch, error) {
	return (records{executor: repository.database}).LoadLaunch(ctx, id)
}

func (repository *Repository) Restore(ctx context.Context, id string) (saves.Restore, error) {
	return (records{executor: repository.database}).Restore(ctx, id)
}

func (repository *Repository) WithWrite(ctx context.Context, work func(saves.WriteScope) error) error {
	err := dbapi.RetryTransaction(ctx, repository.database, func(transaction dbapi.Tx) error {
		bound := writes{records: records{executor: transaction}, transaction: transaction}
		if err := work(saves.WriteScope{
			Launches: bound, Idempotency: bound,
			Checkpoints: bound, GameSaves: bound,
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit checkpoint transaction: %w", err)
	}
	return nil
}

func changed(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("write checkpoint record: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count checkpoint records: %w", err)
	}
	if count != 1 {
		return saves.ErrSyncConflict
	}
	return nil
}
