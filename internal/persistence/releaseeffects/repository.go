// Package releaseeffects provides transaction and fence mechanics for injected domain handlers.
package releaseeffects

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/payloadworker"
	application "retrom/internal/service/payloadrelease"
)

type Repository struct {
	database dbapi.DB
	bind     func(dbapi.Executor) application.EffectScope
}

func New(database dbapi.DB, bind func(dbapi.Executor) application.EffectScope) *Repository {
	return &Repository{database, bind}
}

func (repository *Repository) WithEffects(ctx context.Context, run func(application.EffectScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin domain release: %w", err)
	}
	defer dbapi.Rollback(tx)
	if err := run(repository.bind(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit domain release: %w", err)
	}
	return nil
}

func (repository *Repository) ActiveMutations(ctx context.Context, scope application.Scope) (int64, error) {
	return Mutations(ctx, repository.database, scope)
}

func Mutations(ctx context.Context, executor dbapi.Executor, scope application.Scope) (int64, error) {
	var count int64
	err := dbapi.QueryRowContext(ctx, executor, `SELECT count(*) FROM jobs WHERE scope_type=? AND scope_id=?
 AND kind IN ('GAME_CONTENT_REPLACE','METADATA_SCRAPE','MEDIA_FETCH')
 AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')`, scope.Type, scope.ID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("read active domain mutations: %w", err)
	}
	return count, nil
}

func Bind(executor dbapi.Executor, domain Domain, uploads Uploads) application.EffectScope {
	records := records{executor: executor, domain: domain, uploads: uploads}
	worker := payloadworker.BindWorker(executor)
	return application.EffectScope{Read: records, Write: records, Uploads: uploads.Files, Worker: worker}
}
