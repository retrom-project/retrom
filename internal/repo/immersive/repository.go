package immersive

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/repo/dbexec"
	"retrom/internal/service/immersive"
)

type (
	Repository      struct{ database *sql.DB }
	platformRecords struct{ database dbexec.Executor }
	libraryRecords  struct{ database dbexec.Executor }
	saveRecords     struct{ database dbexec.Executor }
)

func New(database *sql.DB) *Repository { return &Repository{database: database} }

func (repository *Repository) WithRead(ctx context.Context, work func(immersive.ReadScope) error) error {
	transaction, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("immersive: begin read snapshot: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	scope := immersive.ReadScope{
		Platforms: platformRecords{
			transaction,
		},
		Libraries: libraryRecords{
			transaction,
		},
		Saves: saveRecords{
			transaction,
		},
	}
	if err := work(scope); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("immersive: commit read snapshot: %w", err)
	}
	return nil
}

func nullableInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func nullableStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}
