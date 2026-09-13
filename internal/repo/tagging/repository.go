package tagging

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/dbexec"
	"retrom/internal/service/tagging"
)

type (
	Repository      struct{ database *sql.DB }
	tagRecords      struct{ database dbexec.Executor }
	relationRecords struct{ database dbexec.Executor }
	gameRecords     struct{ database dbexec.Executor }
	auditRecords    struct{ database dbexec.Executor }
)

func New(database *sql.DB) *Repository { return &Repository{database: database} }

// Bind returns business capabilities bound to a caller-owned transaction. It never commits it.
func Bind(transaction *sql.Tx) tagging.WriteScope { return writeScope(transaction) }

// BindExecutor returns business capabilities bound to a caller-owned
// transaction or connection. It never commits it.
func BindExecutor(executor dbexec.Executor) tagging.WriteScope { return writeScope(executor) }

func writeScope(database dbexec.Executor) tagging.WriteScope {
	records := tagRecords{database}
	return tagging.WriteScope{
		Tags: records, Changes: records, Relations: relationRecords{database},
		Games: gameRecords{database}, Audit: auditRecords{database},
	}
}

func (repository *Repository) WithWrite(ctx context.Context, work func(tagging.WriteScope) error) error {
	connection, err := repository.database.Conn(ctx)
	if err != nil {
		return fmt.Errorf("tagging: acquire connection: %w", err)
	}
	defer func() { cleanup.Error("close", connection.Close()) }()
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("tagging: begin immediate: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = connection.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		}
	}()
	if err := work(writeScope(connection)); err != nil {
		return err
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("tagging: commit: %w", err)
	}
	committed = true
	return nil
}
