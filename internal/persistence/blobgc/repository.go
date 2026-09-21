package blobgc

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/persistence/blobregistry"
)

type Repository struct{ database *sql.DB }

func New(database *sql.DB) *Repository { return &Repository{database: database} }
func (repository *Repository) Protected(ctx context.Context) (int, error) {
	protected, err := blobregistry.ProtectiveSet(ctx, repository.database)
	if err != nil {
		return 0, fmt.Errorf("blobgc/read protection: %w", err)
	}
	return len(protected), nil
}

func (repository *Repository) Counts(ctx context.Context) (int, int, error) {
	var blobs, candidates int
	if err := repository.database.QueryRowContext(ctx, `SELECT count(*) FROM blobs`).Scan(&blobs); err != nil {
		return 0, 0, fmt.Errorf("blobgc/count blobs: %w", err)
	}
	if err := repository.database.QueryRowContext(
		ctx, `SELECT count(*) FROM blob_gc_candidates`,
	).Scan(&candidates); err != nil {
		return 0, 0, fmt.Errorf("blobgc/count candidates: %w", err)
	}
	return blobs, candidates, nil
}
