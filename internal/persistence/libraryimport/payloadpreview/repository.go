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
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin payloadpreview transaction: %w", err)
	}
	defer dbapi.Rollback(tx)
	records := Records{Executor: tx}
	if err := run(
		application.PreviewExpirationScope{
			Read:  records,
			Write: records,
			DeletionQueue: filedeletion.BindQueue(
				tx,
				application.WorkerScope{},
			),
		},
	); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit payloadpreview transaction: %w", err)
	}
	return nil
}
