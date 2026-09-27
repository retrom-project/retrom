package tagging

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/service/tagging"
)

type (
	Repository      struct{ database dbapi.DB }
	tagRecords      struct{ database dbapi.Executor }
	relationRecords struct{ database dbapi.Executor }
	gameRecords     struct{ database dbapi.Executor }
	auditRecords    struct{ database dbapi.Executor }
)

func New(database dbapi.DB) *Repository { return &Repository{database: database} }

// Bind returns business capabilities bound to a caller-owned transaction. It never commits it.
func Bind(transaction dbapi.Tx) tagging.WriteScope { return writeScope(transaction) }

// BindExecutor returns business capabilities bound to a caller-owned executor.
// It never commits it.
func BindExecutor(executor dbapi.Executor) tagging.WriteScope { return writeScope(executor) }

func writeScope(database dbapi.Executor) tagging.WriteScope {
	records := tagRecords{database}
	return tagging.WriteScope{
		Tags: records, Changes: records, Relations: relationRecords{database},
		Games: gameRecords{database}, Audit: auditRecords{database},
	}
}

func (repository *Repository) WithWrite(ctx context.Context, work func(tagging.WriteScope) error) error {
	transaction, err := repository.database.BeginImmediate(ctx)
	if err != nil {
		return fmt.Errorf("tagging: begin immediate: %w", err)
	}
	defer dbapi.Rollback(transaction)
	if err := work(writeScope(transaction)); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("tagging: commit: %w", err)
	}
	return nil
}
