package pegasusimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	payload "retrom/internal/repo/payloadrelease"
	payloadService "retrom/internal/service/payloadrelease"

	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
	application "retrom/internal/service/pegasusimport"
)

type Completion struct{ database *sql.DB }

func NewCompletion(database *sql.DB) *Completion { return &Completion{database: database} }
func (repository *Completion) WithCompletion(
	ctx context.Context,
	work func(application.CompletionRecords) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Pegasus completion: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := work(completionRecords{tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Pegasus completion: %w", err)
	}
	return nil
}

type completionRecords struct{ tx *sql.Tx }

func (records completionRecords) Current(ctx context.Context, id string) (application.ExecutionSnapshot, error) {
	return leaseRecords(records).Current(ctx, id)
}

func (records completionRecords) Counts(ctx context.Context, id string) (application.CompletionCounts, error) {
	var result application.CompletionCounts
	err := records.tx.QueryRowContext(ctx, `SELECT
count(*) FILTER(WHERE execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT')),
count(*) FILTER(WHERE execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')),
count(*) FILTER(WHERE execution_state='REVIEW_PENDING'),count(*) FILTER(WHERE execution_state='PUBLISHED'),
count(*) FILTER(WHERE execution_state='REVIEW_DISCARDED'),count(*) FILTER(WHERE execution_state='SKIPPED_EXISTING'),
count(*) FILTER(WHERE execution_state='CANCELLED'),
count(*) FILTER(WHERE execution_state IN ('PENDING','COPYING','VALIDATING'))
FROM pegasus_import_items WHERE import_id=?`, id).Scan(&result.Blocked, &result.Failed, &result.ReviewPending,
		&result.Published, &result.ReviewDiscarded, &result.Existing, &result.Cancelled, &result.Unfinished)
	if err != nil {
		return application.CompletionCounts{}, fmt.Errorf("query Pegasus final counts: %w", err)
	}
	return result, nil
}

func (records completionRecords) Complete(ctx context.Context, change application.CompletionChange) error {
	before, counts := change.Before, change.Counts
	result, err := records.tx.ExecContext(ctx, `UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,
heartbeat_at_ms=NULL,worker_id=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND version=? AND state='RUNNING' AND execution_no=? AND attempt_count=? AND worker_id=?
AND leased_until_ms=? AND leased_until_ms>? AND execution_deadline_at_ms=? AND execution_deadline_at_ms>?
AND NOT EXISTS(SELECT 1 FROM pegasus_import_items WHERE import_id=?
AND execution_state IN ('PENDING','COPYING','VALIDATING'))
AND EXISTS(SELECT 1 FROM pegasus_imports plan WHERE plan.id=? AND plan.import_job_id=jobs.id
AND plan.version=? AND plan.state=?)`,
		change.NowMS, change.NowMS, before.JobID, before.JobVersion, before.ExecutionNo, before.Attempt, before.WorkerID,
		before.LeaseUntilMS, change.NowMS, before.DeadlineMS, change.NowMS, before.ImportID,
		before.ImportID, before.ImportVersion, before.ImportState)
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	result, err = recordstore.UpdatePegasusImports(ctx, records.tx, recordstore.Update{
		Set: `state=?,phase=NULL,review_pending_item_count=?,published_item_count=?,review_discarded_item_count=?,
existing_item_count=?,
blocked_item_count=?,failed_item_count=?,cancelled_item_count=?,retryable=?,completed_at_ms=?,
version=version+1,updated_at_ms=?`,
		Values: []any{
			change.ImportState, counts.ReviewPending, counts.Published, counts.ReviewDiscarded, counts.Existing,
			counts.Blocked, counts.Failed, counts.Cancelled, change.Retryable, change.NowMS, change.NowMS,
		},
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND state=? AND import_job_id=?`,
			Args:  []any{before.ImportID, before.ImportVersion, before.ImportState, before.JobID},
		},
	})
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	return records.completionEvent(ctx, change)
}

func (records completionRecords) completionEvent(ctx context.Context, change application.CompletionChange) error {
	counts := change.Counts
	data, err := json.Marshal(map[string]any{
		"schemaVersion": 1, "state": change.ImportState,
		"reviewPending": counts.ReviewPending, "published": counts.Published, "reviewDiscarded": counts.ReviewDiscarded,
		"existing": counts.Existing, "blocked": counts.Blocked, "failed": counts.Failed,
	})
	if err != nil {
		return fmt.Errorf("encode Pegasus completion: %w", err)
	}
	_, err = records.tx.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'SUCCEEDED',?,?)`,
		change.Before.JobID,
		change.Before.ImportID,
		string(data),
		change.NowMS,
	)
	if err != nil {
		return fmt.Errorf("record Pegasus completion: %w", err)
	}
	return nil
}

func (records completionRecords) Payload() payloadService.ReleaseScope {
	return payload.BindReleases(records.tx)
}
