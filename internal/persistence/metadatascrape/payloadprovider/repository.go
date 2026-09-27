package payloadprovider

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/blobgc"
	application "retrom/internal/service/payloadrelease"
)

type Repository struct{ database dbapi.DB }

func New(database dbapi.DB) *Repository { return &Repository{database} }
func (repository *Repository) WithProviderExpiration(
	ctx context.Context,
	run func(
		application.ProviderExpirationScope,
	) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin payloadprovider transaction: %w", err)
	}
	defer dbapi.Rollback(tx)
	records := Records{Executor: tx}
	if err := run(
		application.ProviderExpirationScope{
			Read:  records,
			Write: records,
			GC: blobgc.BindGC(
				tx,
				application.WorkerScope{},
			),
		},
	); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit payloadprovider transaction: %w", err)
	}
	return nil
}
