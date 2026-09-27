package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/persistence/releaseops"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/payloadrelease"
)

type ReleaseEffects struct{ database dbapi.DB }

func NewReleaseEffects(database dbapi.DB) *ReleaseEffects { return &ReleaseEffects{database: database} }

func (repository *ReleaseEffects) WithEffects(ctx context.Context, run func(application.EffectScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin release effects: %w", err)
	}
	defer dbapi.Rollback(tx)
	if err := run(BindEffects(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit release effects: %w", err)
	}
	return nil
}

func (repository *ReleaseEffects) ActiveMutations(ctx context.Context, scope application.Scope) (int64, error) {
	return effectRecords{executor: repository.database}.Mutations(ctx, scope)
}

type effectRecords struct{ executor dbapi.Executor }

func BindEffects(executor dbapi.Executor) application.EffectScope {
	records := effectRecords{executor: executor}
	return application.EffectScope{
		Read:    records,
		Write:   records,
		Uploads: records,
		Worker:  BindWorker(executor),
		GC:      BindGC(executor),
	}
}

func (records effectRecords) Mutations(ctx context.Context, scope application.Scope) (int64, error) {
	var count int64
	err := dbapi.QueryRowContext(ctx, records.executor, `SELECT count(*) FROM jobs WHERE scope_type=? AND scope_id=?
AND kind IN ('GAME_CONTENT_REPLACE','METADATA_SCRAPE','MEDIA_FETCH')
AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')`, scope.Type, scope.ID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("read active payload mutations: %w", err)
	}
	return count, nil
}

func effectCount(result sql.Result, err error, expected int64) error {
	return wrapErr(releaseops.Count(result, err, expected))
}
