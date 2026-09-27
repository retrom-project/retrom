package filecatalog

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
)

type Staging struct{ database dbapi.DB }

func NewStaging(database dbapi.DB) *Staging { return &Staging{database: database} }
func (repository *Staging) Registered(ctx context.Context, id string) (bool, error) {
	var found bool
	err := dbapi.QueryRowContext(ctx, repository.database, `SELECT EXISTS(SELECT 1 FROM stored_files WHERE id=?)`, id).
		Scan(&found)
	if err != nil {
		return false, fmt.Errorf("read registered file: %w", err)
	}
	return found, nil
}

func (repository *Staging) Retire(ctx context.Context, cutoff, now int64) error {
	_, err := repository.database.ExecContext(
		ctx,
		`UPDATE stored_files SET retired_at_ms=? WHERE owner_kind='STAGING' AND retired_at_ms IS NULL AND created_at_ms<?`,
		now,
		cutoff,
	)
	if err != nil {
		return fmt.Errorf("retire abandoned registrations: %w", err)
	}
	return nil
}
