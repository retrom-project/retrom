package emulationstationimport

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
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

func (repository *Starter) CommitStart(ctx context.Context, plan application.StartPlan) (application.Summary, bool, error) {
	var result application.Summary
	var queued bool
	err := dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := startRecords{executor: executor}
		current, err := records.Current(ctx, plan.Before.Summary.ID)
		if err != nil {
			return fmt.Errorf("reread EmulationStation start: %w", err)
		}
		if current.Summary.State != "AWAITING_MAPPING" {
			result = current.Summary
			return nil
		}
		if current.Summary.Version != plan.Before.Summary.Version {
			return application.ErrVersionConflict
		}
		if current.Summary.MappingVersion != plan.Before.Summary.MappingVersion {
			return application.ErrVersionConflict
		}
		if plan.NowMS >= current.Summary.ExpiresAtMS {
			return application.ErrExpired
		}
		mapped := current.Summary.Counts.MappedCollections + current.Summary.Counts.SkippedCollections
		if mapped != current.Summary.Counts.Collections || !current.TagsValid {
			return application.ErrMapping
		}
		if current.Summary.Counts.MappedCollections == 0 {
			return application.ErrNoSelection
		}
		if !current.TargetsValid {
			return application.ErrMappingTargetChanged
		}
		if current.OtherActive {
			return application.ErrActive
		}
		if current.Summary.ImportJobID != nil {
			return application.ErrMapping
		}
		if !application.SameFrozenSource(plan.Before.Summary, current.Summary, plan.Before.FrozenSourceSnapshot, current.FrozenSourceSnapshot) {
			return application.ErrSourceChanged
		}
		plan.Before = current
		if err := records.Queue(ctx, plan); err != nil {
			return fmt.Errorf("queue EmulationStation start: %w", err)
		}
		if err := scheduleTerminalPayloads(ctx, executor, plan.Before.Summary.ID, plan.NowMS); err != nil {
			return err
		}
		after, err := records.Current(ctx, current.Summary.ID)
		if err != nil {
			return fmt.Errorf("read queued EmulationStation plan: %w", err)
		}
		result, queued = after.Summary, true
		return nil
	})
	if err != nil {
		return application.Summary{}, false, err
	}
	return result, queued, nil
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
