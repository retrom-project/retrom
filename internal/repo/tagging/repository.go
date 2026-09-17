package tagging

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/model/tagging"
	"retrom/internal/repo/dbexec"
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

// ApplyReplacementPlan applies a previously planned relation delta to the
// caller-owned transaction. It never commits and never calls application code.
func ApplyReplacementPlan(ctx context.Context, executor dbexec.Executor, plan tagging.ReplacementPlan) error {
	if !plan.Changed {
		return nil
	}
	scope := writeScope(executor)
	if err := scope.Relations.Remove(ctx, plan.Owner, tagging.ReferenceIDs(plan.Removed)); err != nil {
		return fmt.Errorf("tagging: remove owner tags: %w", err)
	}
	if err := scope.Relations.Add(ctx, tagging.Assignment{
		Owner: plan.Owner, References: plan.Added, ActorUserID: plan.ActorUserID, NowMS: plan.NowMS,
	}); err != nil {
		return fmt.Errorf("tagging: add owner tags: %w", err)
	}
	touched := append(tagging.ReferenceIDs(plan.Added), tagging.ReferenceIDs(plan.Removed)...)
	if err := scope.Relations.TouchTags(ctx, plan.ActorUserID, touched, plan.NowMS); err != nil {
		return fmt.Errorf("tagging: touch owner tags: %w", err)
	}
	return nil
}
