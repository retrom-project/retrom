package immersive

import (
	"context"
	"database/sql"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/service/immersive"
)

type (
	Repository      struct{ database dbapi.DB }
	platformRecords struct{ database dbapi.Executor }
	libraryRecords  struct{ database dbapi.Executor }
	saveRecords     struct{ database dbapi.Executor }
)

func New(database dbapi.DB) *Repository { return &Repository{database: database} }

func (repository *Repository) WithRead(ctx context.Context, work func(immersive.ReadScope) error) error {
	transaction, err := repository.database.BeginTx(ctx, &dbapi.TxOptions{ReadOnly: true})
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
