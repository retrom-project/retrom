package uploads

import (
	"context"
	"database/sql"
	"fmt"

	dbapi "retrom/internal/database"
	service "retrom/internal/service/uploads"
)

type (
	Repository          struct{ database dbapi.DB }
	sessionRecords      struct{ executor dbapi.Executor }
	fileRecords         struct{ executor dbapi.Executor }
	partRecords         struct{ executor dbapi.Executor }
	jobRecords          struct{ executor dbapi.Executor }
	blobRecords         struct{ executor dbapi.Executor }
	finalizationRecords struct{ executor dbapi.Executor }
	leaseRecords        struct{ executor dbapi.Executor }
)

func New(database dbapi.DB) *Repository { return &Repository{database: database} }
func (repository *Repository) WithWrite(ctx context.Context, work func(service.WriteScope) error) error {
	// Receiving and assembling bytes happen before this database-only scope.
	err := dbapi.RetryTransaction(ctx, repository.database, func(tx dbapi.Tx) error {
		return work(service.WriteScope{
			Finalize: finalizationRecords{tx}, Leases: leaseRecords{tx},
			Sessions: sessionRecords{tx}, Files: fileRecords{tx}, Parts: partRecords{tx},
			Jobs: jobRecords{tx}, Blobs: blobRecords{tx},
		})
	})
	if err != nil {
		return fmt.Errorf("uploads/commit write: %w", err)
	}
	return nil
}

func requireChange(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("uploads/write state: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("uploads/count state changes: %w", err)
	}
	if changed != 1 {
		return service.ErrInvalid
	}
	return nil
}
