package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/dbexec"
)

type ImportItemRetries struct {
	database      *sql.DB
	preCommitHook func() error
}

func NewImportItemRetries(database *sql.DB) *ImportItemRetries {
	return &ImportItemRetries{database: database}
}

func (repository *ImportItemRetries) WithPreCommitHook(hook func() error) {
	repository.preCommitHook = hook
}

func (repository *ImportItemRetries) LoadRetrySnapshot(
	ctx context.Context, itemID string,
) (application.ImportItemRetrySnapshot, bool, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ImportItemRetrySnapshot{}, false,
			fmt.Errorf("begin import retry snapshot: %w", err)
	}
	defer dbexec.Rollback(tx)
	snapshot, found, err := readRetrySnapshot(ctx, tx, itemID)
	if err != nil {
		return application.ImportItemRetrySnapshot{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return application.ImportItemRetrySnapshot{}, false,
			fmt.Errorf("commit import retry snapshot: %w", err)
	}
	return snapshot, found, nil
}

func (repository *ImportItemRetries) CommitRetry(
	ctx context.Context, write application.ImportItemRetryWrite,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin import item retry: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := writeRetry(ctx, tx, write); err != nil {
		return err
	}
	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit import item retry: %w", err)
	}
	return nil
}

func readRetrySnapshot(
	ctx context.Context, executor dbexec.Executor, itemID string,
) (application.ImportItemRetrySnapshot, bool, error) {
	var result application.ImportItemRetrySnapshot
	err := executor.QueryRowContext(ctx, `
SELECT import_job_id,failed_stage,source_manifest_digest,version,state
FROM import_items WHERE id=?
`, itemID).Scan(&result.ImportID, &result.Stage, &result.ManifestDigest, &result.Version, &result.State)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ImportItemRetrySnapshot{}, false, nil
	}
	if err != nil {
		return application.ImportItemRetrySnapshot{}, false, fmt.Errorf("query import item retry: %w", err)
	}
	return result, true, nil
}

func writeRetry(
	ctx context.Context, executor dbexec.Executor, write application.ImportItemRetryWrite,
) error {
	if _, err := executor.ExecContext(ctx, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms) VALUES(?,'IMPORT_ITEM',?,
'IMPORT_ITEM_PIPELINE',?,1,?,1,'QUEUED',0,2,?,?,?)
`, write.JobID, write.ItemID, write.DedupeKey, write.PayloadJSON, write.NowMS, write.NowMS, write.NowMS); err != nil {
		return fmt.Errorf("create import item retry job: %w", err)
	}
	result, err := executor.ExecContext(ctx, `
UPDATE import_items SET state='QUEUED',failed_stage=NULL,last_error_code=NULL,
version=version+1,updated_at_ms=?
WHERE id=? AND state='FAILED_RETRYABLE' AND version=?
`, write.NowMS, write.ItemID, write.ExpectedVersion)
	if err != nil {
		return fmt.Errorf("queue import item retry: %w", err)
	}
	if changed, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("queue import item retry result: %w", err)
	} else if changed != 1 {
		return application.ErrInvalid
	}
	result, err = executor.ExecContext(ctx, `
UPDATE import_jobs SET failed_item_count=failed_item_count-1,queued_item_count=queued_item_count+1,
state='RUNNING',version=version+1,updated_at_ms=?
WHERE id=? AND failed_item_count>0
`, write.NowMS, write.ImportID)
	if err != nil {
		return fmt.Errorf("queue import retry aggregate: %w", err)
	}
	if changed, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("queue import retry aggregate result: %w", err)
	} else if changed != 1 {
		return application.ErrInvalid
	}
	if _, err := executor.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'MANUAL_RETRY','{}',?)
`, write.JobID, write.ItemID, write.NowMS); err != nil {
		return fmt.Errorf("record import item retry event: %w", err)
	}
	return nil
}

var _ application.ImportItemRetryRepository = (*ImportItemRetries)(nil)
