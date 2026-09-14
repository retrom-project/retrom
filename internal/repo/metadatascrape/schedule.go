package metadatascrape

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/model/metadatascrape"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
)

type (
	ScheduleRepository struct{ database *sql.DB }
	scheduleReads      struct{ database dbexec.Executor }
	scheduleWrites     struct{ transaction *sql.Tx }
)

func NewScheduler(database *sql.DB) *ScheduleRepository {
	return &ScheduleRepository{database: database}
}

func BindSchedule(transaction *sql.Tx) metadatascrape.ScheduleScope {
	reader := scheduleReads{transaction}
	return metadatascrape.ScheduleScope{Subjects: reader, Sources: reader, Writes: scheduleWrites{transaction}}
}

func (repository *ScheduleRepository) WithWrite(
	ctx context.Context,
	work func(metadatascrape.ScheduleScope) error,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin scrape scheduling: %w", err)
	}
	defer dbexec.Rollback(transaction)
	if err := work(BindSchedule(transaction)); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit scrape scheduling: %w", err)
	}
	return nil
}

func (writes scheduleWrites) Create(ctx context.Context, plan metadatascrape.SchedulePlan) error {
	_, err := writes.transaction.ExecContext(ctx, `INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,
 payload_json,cancellable,state,attempt_count,max_attempts,available_at_ms,finished_at_ms,created_at_ms,updated_at_ms)
 VALUES(?,?,?,'METADATA_SCRAPE',?,1,?,1,?,0,4,?,?,?,?)`, plan.JobID, plan.Subject.Kind, plan.Subject.ID, plan.Dedupe,
		plan.PayloadJSON, plan.JobState, plan.Now, plan.FinishedAt, plan.Now, plan.Now)
	if err != nil {
		return fmt.Errorf("insert scrape job: %w", err)
	}
	var itemID, gameID *string
	if plan.Subject.Kind == "GAME" {
		gameID = &plan.Subject.ID
	} else {
		itemID = &plan.Subject.ID
	}
	_, err = writes.transaction.ExecContext(
		ctx,
		`INSERT INTO metadata_scrape_runs
 (id,import_item_id,game_id,job_id,provider,provider_config_version,state,created_at_ms,updated_at_ms,completed_at_ms)
 VALUES(?,?,?,?,?,1,?,?,?,?)`,
		plan.RunID,
		itemID,
		gameID,
		plan.JobID,
		plan.Provider,
		plan.RunState,
		plan.Now,
		plan.Now,
		plan.FinishedAt,
	)
	if err != nil {
		return fmt.Errorf("insert scrape run: %w", err)
	}
	_, err = writes.transaction.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
 VALUES(?,?,?,?,?,?)`,
		plan.JobID,
		plan.Subject.Kind,
		plan.Subject.ID,
		plan.JobState,
		plan.EventJSON,
		plan.Now,
	)
	if err != nil {
		return fmt.Errorf("insert scrape scheduled event: %w", err)
	}
	return nil
}

func (writes scheduleWrites) Evidence(ctx context.Context, evidence []metadatascrape.HashEvidence) error {
	for _, item := range evidence {
		_, err := writes.transaction.ExecContext(ctx, `INSERT INTO content_hash_evidence
 (id,scrape_run_id,profile,blob_id,archive_blob_id,archive_entry_ordinal,
 crc32,md5,sha1,sha256,query_order,created_at_ms)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, item.ID, item.RunID, item.Profile, item.BlobID,
			item.ArchiveBlobID, item.ArchiveOrdinal,
			item.CRC32, item.MD5, item.SHA1, item.SHA256, item.Order, item.Now)
		if err != nil {
			return fmt.Errorf("insert content hash evidence: %w", err)
		}
	}
	return nil
}

func (writes scheduleWrites) Game(ctx context.Context, id string, version, now int64) error {
	result, err := recordstore.UpdateGames(ctx, writes.transaction, recordstore.Update{
		Set: `version=version+1,updated_at_ms=?`, Values: []any{
			now,
		}, Scope: recordstore.Scope{
			Where: `id=? AND version=?`,
			Args: []any{
				id,
				version,
			},
		},
	})
	return scheduleChanged(result, err, metadatascrape.ErrGameVersionConflict)
}

func (writes scheduleWrites) Review(ctx context.Context, value metadatascrape.ReviewChange) error {
	result, err := recordstore.UpdateReviewDrafts(ctx, writes.transaction, recordstore.Update{
		Set: `version=version+1,updated_at_ms=?`, Values: []any{value.Now},
		Scope: recordstore.Scope{Where: `import_item_id=? AND version=?`, Args: []any{value.ItemID, value.Version}},
	})
	if err := scheduleChanged(result, err, metadatascrape.ErrReviewVersionConflict); err != nil {
		return err
	}
	_, err = recordstore.CreateReviewEvents(
		ctx,
		writes.transaction,
		`INSERT INTO review_events
 (id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,before_json,after_json,diff_json,
 config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms)
 VALUES(?,?,'SCRAPE_REQUESTED',?,?,?,?,?,?,'{"schemaVersion":2}','{"schemaVersion":2}',?,?)`,

		value.ID,
		value.ItemID,
		value.Actor.Kind,
		value.Actor.UserID,
		value.Actor.Label,
		value.BeforeJSON,
		value.AfterJSON,
		value.AfterJSON,
		value.AfterJSON,
		value.Now,
	)
	if err != nil {
		return fmt.Errorf("insert scrape review event: %w", err)
	}
	return nil
}

func scheduleChanged(result sql.Result, err, errorConflict error) error {
	if err != nil {
		return fmt.Errorf("update scrape subject: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count scrape subject update: %w", err)
	}
	if changed != 1 {
		return fmt.Errorf("scrape subject changed: %w", errorConflict)
	}
	return nil
}
