package maintenance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/model/maintenance"
	"retrom/internal/repo/blobregistry"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/store"
)

type (
	Repository struct{}
	writes     struct{ transaction *sql.Tx }
)

func New() *Repository { return &Repository{} }
func (repository *Repository) CurrentLineage() (maintenance.Lineage, error) {
	lineage, err := store.CurrentMigrationLineage()
	if err != nil {
		return maintenance.Lineage{}, fmt.Errorf("read current backup lineage: %w", err)
	}
	return maintenance.Lineage{Version: lineage.Version, Digest: lineage.Digest}, nil
}

func inspectLineage(ctx context.Context, database *sql.DB) (maintenance.Lineage, error) {
	if err := checkDatabase(ctx, database); err != nil {
		return maintenance.Lineage{}, err
	}
	lineage, err := store.ValidateCurrentMigrationLineage(ctx, database)
	if err != nil {
		if ctx.Err() != nil {
			return maintenance.Lineage{}, fmt.Errorf("read backup lineage: %w", ctx.Err())
		}
		return maintenance.Lineage{}, fmt.Errorf("%w: %w", maintenance.ErrInvalidBundle, err)
	}
	if err := blobregistry.ValidateSchema(ctx, database); err != nil {
		return maintenance.Lineage{}, fmt.Errorf("validate backup references: %w", err)
	}
	return maintenance.Lineage{Version: lineage.Version, Digest: lineage.Digest}, nil
}

func (repository *Repository) Checkpoint(ctx context.Context, path string) error {
	return withDatabase(ctx, path, func(database *sql.DB) error {
		if _, err := inspectLineage(ctx, database); err != nil {
			return err
		}
		var busy, frames, checkpointed int
		err := database.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &frames, &checkpointed)
		if err != nil {
			return fmt.Errorf("checkpoint backup database: %w", err)
		}
		if busy != 0 {
			return maintenance.ErrCheckpointFailed
		}
		return nil
	})
}

func (repository *Repository) Inspect(ctx context.Context, path string) (maintenance.Snapshot, error) {
	var snapshot maintenance.Snapshot
	err := withDatabase(ctx, path, func(database *sql.DB) error {
		var err error
		snapshot.Lineage, err = inspectLineage(ctx, database)
		if err != nil {
			return err
		}
		snapshot.Blobs, err = backupBlobs(ctx, database)
		if err != nil {
			return err
		}
		snapshot.Parts, err = backupParts(ctx, database)
		return err
	})
	return snapshot, err
}

func (repository *Repository) WithRestore(
	ctx context.Context, path string, work func(maintenance.RestoreRecords) error,
) error {
	return withDatabase(ctx, path, func(database *sql.DB) error {
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin restore security boundary: %w", err)
		}
		defer dbexec.Rollback(tx)
		if err := work(writes{tx}); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit restore security boundary: %w", err)
		}
		return nil
	})
}

func withDatabase(ctx context.Context, path string, work func(*sql.DB) error) error {
	database, err := openDatabase(ctx, path)
	if err != nil {
		return err
	}
	// Close also runs if a caller panics; sql.DB.Close is idempotent.
	defer func() { cleanup.Error("close maintenance database", database.Close()) }()
	workErr := work(database)
	if err := database.Close(); err != nil {
		return errors.Join(workErr, fmt.Errorf("close maintenance database: %w", err))
	}
	return workErr
}

func affected(results ...sql.Result) ([]int64, error) {
	counts := make([]int64, len(results))
	for index, result := range results {
		count, err := result.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("count restored records: %w", err)
		}
		counts[index] = count
	}
	return counts, nil
}
