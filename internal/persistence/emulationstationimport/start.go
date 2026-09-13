package emulationstationimport

import (
	"context"
	"database/sql"
	"fmt"

	payload "retrom/internal/persistence/payloadrelease"

	"retrom/internal/persistence/dbexec"
	application "retrom/internal/service/emulationstationimport"
)

type Starter struct{ database *sql.DB }

func NewStarter(database *sql.DB) *Starter { return &Starter{database: database} }
func (repository *Starter) Inspect(ctx context.Context, id string) (application.StartSnapshot, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.StartSnapshot{}, fmt.Errorf("begin EmulationStation start inspection: %w", err)
	}
	defer dbexec.Rollback(tx)
	result, err := (startRecords{transaction: tx, executor: tx}).Current(ctx, id)
	if err != nil {
		return application.StartSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.StartSnapshot{}, fmt.Errorf("finish EmulationStation start inspection: %w", err)
	}
	return result, nil
}

func (repository *Starter) WithStart(ctx context.Context, work func(application.StartScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin EmulationStation start: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := startRecords{transaction: tx, executor: tx}
	if err := work(application.StartScope{Payload: payload.BindReleases(tx), Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit EmulationStation start: %w", err)
	}
	return nil
}

type startRecords struct {
	transaction *sql.Tx
	executor    dbexec.Executor
}

func (records startRecords) Current(ctx context.Context, id string) (application.StartSnapshot, error) {
	summary, err := (&Queries{database: records.executor}).Get(ctx, id)
	if err != nil {
		return application.StartSnapshot{}, err
	}
	result := application.StartSnapshot{Summary: summary}
	if summary.State != "AWAITING_MAPPING" {
		return result, nil
	}
	result.FrozenSourceSnapshot, err = (frozenSourceRecords{executor: records.executor}).Read(ctx, id)
	if err != nil {
		return application.StartSnapshot{}, err
	}
	err = records.executor.QueryRowContext(ctx, `SELECT
NOT EXISTS(SELECT 1 FROM emulationstation_import_collections collection
JOIN json_each(collection.tag_snapshot_json) entry
LEFT JOIN tags tag ON tag.id=json_extract(entry.value,'$.tagId') AND tag.status='ACTIVE'
WHERE collection.import_id=? AND collection.mapping_action='IMPORT' AND tag.id IS NULL),
EXISTS(SELECT 1 FROM emulationstation_imports active WHERE active.id<>?
AND active.import_job_id IS NOT NULL AND active.state IN ('QUEUED','RUNNING','CANCEL_REQUESTED'))`, id, id).Scan(
		&result.TagsValid, &result.OtherActive,
	)
	if err != nil {
		return application.StartSnapshot{}, fmt.Errorf("read EmulationStation start readiness: %w", err)
	}
	result.TargetsValid, err = (frozenSourceRecords{executor: records.executor}).targetsValid(ctx, id)
	if err != nil {
		return application.StartSnapshot{}, err
	}
	return result, nil
}
