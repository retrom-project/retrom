package uploads

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/repo/dbexec"
	service "retrom/internal/service/uploads"
)

type (
	Repository          struct{ database *sql.DB }
	sessionRecords      struct{ executor dbexec.Executor }
	fileRecords         struct{ executor dbexec.Executor }
	partRecords         struct{ executor dbexec.Executor }
	jobRecords          struct{ executor dbexec.Executor }
	blobRecords         struct{ executor dbexec.Executor }
	finalizationRecords struct{ executor dbexec.Executor }
	leaseRecords        struct{ executor dbexec.Executor }
)

func New(database *sql.DB) *Repository { return &Repository{database: database} }
func (repository *Repository) WithWrite(ctx context.Context, work func(service.WriteScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("uploads/begin write: %w", err)
	}
	defer dbexec.Rollback(tx)
	scope := service.WriteScope{
		Finalize: finalizationRecords{tx}, Leases: leaseRecords{tx},
		Sessions: sessionRecords{
			tx,
		},
		Files: fileRecords{
			tx,
		},
		Parts: partRecords{
			tx,
		},
		Jobs: jobRecords{
			tx,
		},
		Blobs: blobRecords{
			tx,
		},
	}
	if err := work(scope); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
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
