package pegasusimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	payload "retrom/internal/persistence/payloadrelease"

	"retrom/internal/cleanup"
	"retrom/internal/dbexec"
	library "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/pegasusimport"
)

type Recovery struct{ database *sql.DB }

func NewRecovery(database *sql.DB) *Recovery { return &Recovery{database: database} }
func (repository *Recovery) WithRecovery(ctx context.Context, work func(application.RecoveryScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Pegasus recovery: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := work(
		application.RecoveryScope{Payload: payload.BindReleases(tx), Records: recoveryRecords{tx}, Metadata: library.BindMetadata(tx)},
	); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Pegasus recovery: %w", err)
	}
	return nil
}

const recoverySnapshotSQL = `SELECT job.id,plan.id,job.kind,job.state,plan.state,COALESCE(job.worker_id,''),
job.version,plan.version,job.execution_no,job.attempt_count,job.max_attempts,
COALESCE(job.leased_until_ms,0),COALESCE(job.execution_deadline_at_ms,0)
FROM jobs job JOIN pegasus_imports plan ON plan.id=job.scope_id AND
 ((job.kind='SERVER_PEGASUS_SCAN' AND job.id=plan.scan_job_id AND plan.import_job_id IS NULL)
 OR (job.kind='SERVER_PEGASUS_IMPORT' AND job.id=plan.import_job_id))
WHERE job.scope_type='PEGASUS_IMPORT'`

func (repository *Recovery) ExpiredExecutions(
	ctx context.Context,
	now int64,
	limit int,
) ([]application.RecoverySnapshot, error) {
	rows, err := repository.database.QueryContext(ctx, recoverySnapshotSQL+`
AND ((job.state IN ('RUNNING','CANCEL_REQUESTED') AND job.leased_until_ms<=?)
OR (job.state='QUEUED' AND job.leased_until_ms IS NULL AND job.worker_id IS NULL AND job.attempt_count>0
AND (job.execution_deadline_at_ms<=? OR job.attempt_count>=job.max_attempts)))
ORDER BY job.leased_until_ms,job.id LIMIT ?`, now, now, limit)
	if err != nil {
		return nil, fmt.Errorf("query expired Pegasus executions: %w", err)
	}
	defer func() { cleanup.Error("close expired Pegasus executions", rows.Close()) }()
	result := []application.RecoverySnapshot{}
	for rows.Next() {
		current, err := scanRecovery(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, current)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired Pegasus executions: %w", err)
	}
	return result, nil
}

func scanRecovery(scanner dbexec.Scanner) (application.RecoverySnapshot, error) {
	var value application.RecoverySnapshot
	err := scanner.Scan(
		&value.JobID,
		&value.ImportID,
		&value.Kind,
		&value.JobState,
		&value.ImportState,
		&value.WorkerID,

		&value.JobVersion,
		&value.ImportVersion,
		&value.ExecutionNo,
		&value.Attempt,
		&value.MaxAttempts,
		&value.LeaseUntilMS,
		&value.DeadlineMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.RecoverySnapshot{}, application.ErrNotFound
	}
	if err != nil {
		return application.RecoverySnapshot{}, fmt.Errorf("read Pegasus recovery execution: %w", err)
	}
	return value, nil
}

type recoveryRecords struct{ tx *sql.Tx }

func (records recoveryRecords) Current(ctx context.Context, id string) (application.RecoverySnapshot, error) {
	return scanRecovery(records.tx.QueryRowContext(ctx, recoverySnapshotSQL+` AND job.id=?`, id))
}

func (records recoveryRecords) Reviews(
	ctx context.Context,
	importID string,
	limit int,
) ([]application.ReviewHandoffSnapshot, error) {
	ids, err := records.reviewIDs(ctx, importID, limit)
	if err != nil {
		return nil, err
	}
	result := make([]application.ReviewHandoffSnapshot, 0, len(ids))
	for _, id := range ids {
		current, err := (reviewHandoffRecords{records.tx}).CurrentReviewHandoff(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, current)
	}
	return result, nil
}

func (records recoveryRecords) reviewIDs(ctx context.Context, importID string, limit int) ([]string, error) {
	rows, err := records.tx.QueryContext(ctx, `SELECT source.id FROM pegasus_import_items source
 JOIN import_items item ON item.id=source.library_import_item_id AND item.import_job_id=source.library_import_job_id
 WHERE source.import_id=? AND source.execution_state IN ('PENDING','COPYING','VALIDATING')
 AND item.state='REVIEW_PENDING' ORDER BY source.id LIMIT ?`, importID, limit)
	if err != nil {
		return nil, fmt.Errorf("query interrupted Pegasus reviews: %w", err)
	}
	defer func() { cleanup.Error("close interrupted Pegasus reviews", rows.Close()) }()
	result := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan interrupted Pegasus review: %w", err)
		}
		result = append(result, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate interrupted Pegasus reviews: %w", err)
	}
	return result, nil
}
