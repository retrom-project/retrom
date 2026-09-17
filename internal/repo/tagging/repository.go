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

// BindReferenceReader returns a read-only ReferenceReader bound to a
// caller-owned transaction or connection.
func BindReferenceReader(executor dbexec.Executor) tagging.ReferenceReader {
	return relationRecords{database: executor}
}

func newWriteScope(database dbexec.Executor) writeScope {
	records := tagRecords{database}
	return writeScope{
		tags: records, changes: records, relations: relationRecords{database},
		games: gameRecords{database}, audit: auditRecords{database},
	}
}

// ApplyReplacementPlan applies a previously planned relation delta to the
// caller-owned transaction. It never commits and never calls application code.
func ApplyReplacementPlan(ctx context.Context, executor dbexec.Executor, plan tagging.ReplacementPlan) error {
	if !plan.Changed {
		return nil
	}
	scope := newWriteScope(executor)
	if err := scope.relations.Remove(ctx, plan.Owner, tagging.ReferenceIDs(plan.Removed)); err != nil {
		return fmt.Errorf("tagging: remove owner tags: %w", err)
	}
	if err := scope.relations.Add(ctx, tagging.Assignment{
		Owner: plan.Owner, References: plan.Added, ActorUserID: plan.ActorUserID, NowMS: plan.NowMS,
	}); err != nil {
		return fmt.Errorf("tagging: add owner tags: %w", err)
	}
	touched := append(tagging.ReferenceIDs(plan.Added), tagging.ReferenceIDs(plan.Removed)...)
	if err := scope.relations.TouchTags(ctx, plan.ActorUserID, touched, plan.NowMS); err != nil {
		return fmt.Errorf("tagging: touch owner tags: %w", err)
	}
	return nil
}
