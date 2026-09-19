package emulationstationimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
)

type Recovery struct{ database *sql.DB }

func NewRecovery(database *sql.DB) *Recovery { return &Recovery{database: database} }

func (repository *Recovery) Expired(ctx context.Context, now int64, limit int) ([]application.LeaseSnapshot, error) {
	rows, err := repository.database.QueryContext(ctx, leaseSnapshotSQL+`
AND ((job.state IN ('RUNNING','CANCEL_REQUESTED') AND job.leased_until_ms<=?)
OR (job.state='QUEUED' AND job.leased_until_ms IS NULL AND job.worker_id IS NULL AND job.attempt_count>0
AND (job.execution_deadline_at_ms<=? OR job.attempt_count>=job.max_attempts)))
ORDER BY job.leased_until_ms,job.id LIMIT ?`, now, now, limit)
	if err != nil {
		return nil, fmt.Errorf("list expired EmulationStation jobs: %w", err)
	}
	defer func() { cleanup.Error("close expired EmulationStation jobs", rows.Close()) }()
	result := []application.LeaseSnapshot{}
	for rows.Next() {
		value, _, err := scanLease(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired EmulationStation jobs: %w", err)
	}
	return result, nil
}

type recoveryRecords struct {
	transaction *sql.Tx
	executor    dbexec.Executor
}

func (records recoveryRecords) Current(ctx context.Context, id string) (application.LeaseSnapshot, bool, error) {
	return (leaseRecords{executor: records.executor}).Current(ctx, id)
}

func (records recoveryRecords) fence(ctx context.Context, change application.RecoveryChange) error {
	before := change.Before
	result, err := records.executor.ExecContext(ctx, `UPDATE jobs SET version=version WHERE id=? AND version=?
AND state=? AND kind=? AND scope_type='EMULATIONSTATION_IMPORT' AND scope_id=?
AND execution_no=? AND attempt_count=? AND max_attempts=? AND COALESCE(worker_id,'')=?
AND COALESCE(leased_until_ms,0)=? AND execution_started_at_ms IS ? AND execution_deadline_at_ms=?
AND ((state IN ('RUNNING','CANCEL_REQUESTED') AND leased_until_ms<=?)
OR (state='QUEUED' AND leased_until_ms IS NULL AND worker_id IS NULL
AND (execution_deadline_at_ms<=? OR attempt_count>=max_attempts)))
AND EXISTS(SELECT 1 FROM emulationstation_imports plan WHERE plan.id=? AND plan.version=? AND plan.state=?
AND plan.root_id=? AND plan.root_config_digest=? AND plan.source_relative_path=?
AND plan.created_by_user_id=? AND plan.release_year_max=?
AND ((jobs.kind='SERVER_EMULATIONSTATION_SCAN' AND plan.scan_job_id=jobs.id AND plan.import_job_id IS NULL)
OR (jobs.kind='SERVER_EMULATIONSTATION_IMPORT' AND plan.import_job_id=jobs.id)))`,
		before.JobID, before.JobVersion, before.JobState, before.Kind, before.ImportID, before.ExecutionNo, before.Attempt,
		before.MaxAttempts, before.WorkerID, before.LeaseUntilMS, before.StartedAtMS, before.DeadlineAtMS,
		change.NowMS, change.NowMS,
		before.ImportID, before.ImportVersion, before.ImportState, before.RootID, before.RootDigest, before.RelativePath,
		before.CreatedByUserID, before.ReleaseYearMax)
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}
