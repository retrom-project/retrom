package gamecontent

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/model/gamecontent"
	payloadmodel "retrom/internal/model/payloadrelease"
	validation "retrom/internal/repo/corevalidation"
	"retrom/internal/repo/dbexec"
)

type Repository struct {
	database         *sql.DB
	gc               payloadmodel.GCStager
	publishPreCommit func() error
}
type (
	records struct{ executor dbexec.Executor }
	writes  struct {
		transaction *sql.Tx
	}
)

func New(database *sql.DB) *Repository {
	return &Repository{database: database}
}

func (repository *Repository) WithGCStager(gc payloadmodel.GCStager) *Repository {
	repository.gc = gc
	return repository
}

// WithPublishPreCommitHook sets a test-only hook that runs just before
// CommitPublish commits its transaction.
func WithPublishPreCommitHook(repo *Repository, hook func() error) {
	repo.publishPreCommit = hook
}

func readScope(executor dbexec.Executor) gamecontent.ReadScope {
	bound := records{executor}
	return gamecontent.ReadScope{
		Content: bound,
		Inputs:  bound,
		BIOS:    validation.New(executor),
		Admin:   bound,
	}
}

func writeScope(tx *sql.Tx) gamecontent.WriteScope {
	bound := writes{tx}
	read := readScope(tx)
	return gamecontent.WriteScope{
		ReadScope:          read,
		Replays:            bound,
		Jobs:               bound,
		Leases:             bound,
		ContentWriter:      bound,
		Retirements:        BindRetirement(tx),
		AdminWriter:        bound,
		GameDeletionReader: bound,
		GameDeletionWriter: bound,
	}
}

func beginWrite(ctx context.Context, db *sql.DB) (*sql.Tx, gamecontent.WriteScope, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, gamecontent.WriteScope{}, fmt.Errorf("begin content write: %w", err)
	}
	return tx, writeScope(tx), nil
}

func commitScope(tx *sql.Tx) error {
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit content write: %w", err)
	}
	return nil
}

func changed(result sql.Result, err error) (bool, error) {
	if err != nil {
		return false, fmt.Errorf("write replacement record: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count replacement writes: %w", err)
	}
	return count == 1, nil
}

func requireChanged(result sql.Result, err error) error {
	ok, err := changed(result, err)
	if err != nil {
		return err
	}
	if !ok {
		return gamecontent.ErrExecutionLost
	}
	return nil
}
