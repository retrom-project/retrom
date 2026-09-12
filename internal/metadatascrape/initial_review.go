package metadatascrape

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/dbexec"
	initialpersistence "retrom/internal/persistence/metadatascrape"
	initialservice "retrom/internal/service/metadatascrape"
)

func (service *Service) completeInitialImport(ctx context.Context, transaction *sql.Tx, runID string, now int64) error {
	if err := initialservice.NewInitialReview(
		initialpersistence.BindInitialReview(
			transaction,
		),
	).Complete(
		ctx,
		runID,
		now,
	); err != nil {
		return fmt.Errorf("complete initial scrape review: %w", err)
	}
	return nil
}

func (service *Service) complete(ctx context.Context, runID, jobID string, candidateCount int) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	defer dbexec.Rollback(transaction)
	now := service.now().UnixMilli()
	if err := service.completeInitialImport(ctx, transaction, runID, now); err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE metadata_scrape_runs
SET state='COMPLETED',
version=version+1,
updated_at_ms=?,
completed_at_ms=?
WHERE id=?
AND state='RUNNING'
`, now, now, runID); err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='SUCCEEDED',
finished_at_ms=?,
leased_until_ms=NULL,
heartbeat_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
`, now, now, now, jobID); err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	data := fmt.Sprintf(`{"candidateCount":%d}`, candidateCount)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) SELECT id,
scope_type,
scope_id,
'SUCCEEDED',
?,
?
FROM jobs
WHERE id=?
`, data, now, jobID); err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit metadata asset fetch: %w", err)
	}
	return nil
}

func failInitialImport(ctx context.Context, transaction *sql.Tx, runID, code string, now int64) error {
	if err := initialservice.NewInitialReview(
		initialpersistence.BindInitialReview(
			transaction,
		),
	).Fail(
		ctx,
		runID,
		code,
		now,
	); err != nil {
		return fmt.Errorf("fail initial scrape review: %w", err)
	}
	return nil
}

func (service *Service) fail(ctx context.Context, runID, jobID, code string, cause error) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: %w (persist failure: %w)", code, cause, err)
	}
	defer dbexec.Rollback(transaction)
	now := service.now().UnixMilli()
	if _, err := transaction.ExecContext(
		ctx,
		`
UPDATE metadata_scrape_runs
SET state='FAILED',
error_code=?,
version=version+1,
updated_at_ms=?,
completed_at_ms=?
WHERE id=?
AND state='RUNNING'
`,
		code,
		now,
		now,
		runID,
	); err != nil {
		return fmt.Errorf("%s: %w (persist failure: %w)", code, cause, err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`
UPDATE jobs
SET state='FAILED',
error_code=?,
error_retryable=1,
finished_at_ms=?,
leased_until_ms=NULL,
version=version+1,
updated_at_ms=?
WHERE id=?
`,
		code,
		now,
		now,
		jobID,
	); err != nil {
		return fmt.Errorf("%s: %w (persist failure: %w)", code, cause, err)
	}
	if err := failInitialImport(ctx, transaction, runID, code, now); err != nil {
		return fmt.Errorf("%s: %w (persist failure: %w)", code, cause, err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) SELECT id,
scope_type,
scope_id,
'FAILED',
?,
?
FROM jobs
WHERE id=?
`,
		fmt.Sprintf(`{"code":%q}`, code),
		now,
		jobID,
	); err != nil {
		return fmt.Errorf("%s: %w (persist failure: %w)", code, cause, err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("%s: %w (persist failure: %w)", code, cause, err)
	}
	return fmt.Errorf("%s: %w", code, cause)
}
