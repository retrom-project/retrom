package payloadlaunch

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/cleanupjobs"
)

type Repository struct{ database dbapi.DB }

func New(database dbapi.DB) *Repository { return &Repository{database} }
func (repository *Repository) WithLaunchRetirement(
	ctx context.Context,
	run func(
		application.LaunchRetirementScope,
	) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin payloadlaunch transaction: %w", err)
	}
	defer dbapi.Rollback(tx)
	records := Records{Executor: tx}
	if err := run(application.LaunchRetirementScope{Read: records, Launch: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit payloadlaunch transaction: %w", err)
	}
	return nil
}
