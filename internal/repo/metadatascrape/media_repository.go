package metadatascrape

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/model/metadatascrape"
	"retrom/internal/repo/dbexec"
)

type (
	MediaRepository struct{ database *sql.DB }
	mediaRecords    struct{ executor dbexec.Executor }
)

func NewMedia(database *sql.DB) *MediaRepository { return &MediaRepository{database} }

func (repository *MediaRepository) WithWrite(ctx context.Context, work func(metadatascrape.MediaScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin media transaction: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := mediaRecords{tx}
	if err := work(metadatascrape.MediaScope{Read: records, Leases: records, Assets: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit media transaction: %w", err)
	}
	return nil
}

func mediaChanged(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("write media record: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count media records: %w", err)
	}
	if count != 1 {
		return metadatascrape.ErrExecutionLost
	}
	return nil
}

func (records mediaRecords) event(ctx context.Context, id, kind, data string, now int64) error {
	return mediaChanged(records.executor.ExecContext(ctx, `
INSERT INTO job_events
 (job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
 SELECT id,scope_type,scope_id,?,?,? FROM jobs WHERE id=? AND kind='MEDIA_FETCH'`, kind, data, now, id))
}
