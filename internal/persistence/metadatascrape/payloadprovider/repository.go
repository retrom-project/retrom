package payloadprovider

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/filedeletion"
	application "retrom/internal/service/cleanupjobs"
)

type Repository struct{ database dbapi.DB }

func New(database dbapi.DB) *Repository { return &Repository{database} }
func (repository *Repository) WithProviderExpiration(
	ctx context.Context,
	run func(
		application.ProviderExpirationScope,
	) error,
) error {
	err := dbapi.RetryTransaction(ctx, repository.database, func(tx dbapi.Tx) error {
		records := Records{Executor: tx}
		if err := run(
			application.ProviderExpirationScope{
				Read:          records,
				Write:         records,
				DeletionQueue: filedeletion.Bind(tx),
			},
		); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit payloadprovider transaction: %w", err)
	}
	return nil
}
