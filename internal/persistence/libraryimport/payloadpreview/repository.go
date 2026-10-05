package payloadpreview

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/filedeletion"
	application "retrom/internal/service/cleanupjobs"
)

type Repository struct{ database dbapi.DB }

func New(database dbapi.DB) *Repository { return &Repository{database} }
func (repository *Repository) WithPreviewExpiration(
	ctx context.Context,
	run func(
		application.PreviewExpirationScope,
	) error,
) error {
	err := dbapi.RetryTransaction(ctx, repository.database, func(tx dbapi.Tx) error {
		records := Records{Executor: tx}
		if err := run(
			application.PreviewExpirationScope{
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
		return fmt.Errorf("commit payloadpreview transaction: %w", err)
	}
	return nil
}
