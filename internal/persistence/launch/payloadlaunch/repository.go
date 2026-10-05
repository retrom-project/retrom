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
	err := dbapi.RetryTransaction(ctx, repository.database, func(tx dbapi.Tx) error {
		records := Records{Executor: tx}
		if err := run(application.LaunchRetirementScope{Read: records, Launch: records}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit payloadlaunch transaction: %w", err)
	}
	return nil
}
