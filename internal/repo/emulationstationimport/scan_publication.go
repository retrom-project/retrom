package emulationstationimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/repo/dbexec"
	application "retrom/internal/service/emulationstationimport"
)

type ScanPublication struct{ database *sql.DB }

func NewScanPublication(database *sql.DB) *ScanPublication {
	return &ScanPublication{database: database}
}

func (repository *ScanPublication) WithScan(ctx context.Context, work func(application.ScanScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin EmulationStation scan transaction: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := scanRecords{executor: tx}
	if err := work(application.ScanScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit EmulationStation scan transaction: %w", err)
	}
	return nil
}

type scanRecords struct{ executor dbexec.Executor }

func (records scanRecords) Current(ctx context.Context, id string) (application.LeaseSnapshot, bool, error) {
	return leaseRecords(records).Current(ctx, id)
}

func (records scanRecords) fence(ctx context.Context, change application.ScanMutation) error {
	before := change.Before
	result, err := records.executor.ExecContext(ctx, `UPDATE jobs SET version=version WHERE id=? AND version=?
AND scope_type='EMULATIONSTATION_IMPORT' AND scope_id=? AND kind='SERVER_EMULATIONSTATION_SCAN' AND state='RUNNING'
AND execution_no=? AND attempt_count=? AND worker_id=? AND leased_until_ms=? AND leased_until_ms>?
AND execution_started_at_ms IS ? AND execution_deadline_at_ms=? AND execution_deadline_at_ms>?
AND EXISTS(SELECT 1 FROM emulationstation_imports plan WHERE plan.id=? AND plan.version=? AND plan.state='SCANNING'
AND plan.scan_job_id=jobs.id AND plan.import_job_id IS NULL AND plan.source_snapshot_digest IS NULL
AND plan.scan_completed_at_ms IS NULL AND plan.release_year_max=?
AND plan.root_id=? AND plan.root_config_digest=? AND plan.source_relative_path=? AND plan.created_by_user_id=?)`,
		before.JobID, before.JobVersion, before.ImportID,
		before.ExecutionNo, before.Attempt, before.WorkerID, before.LeaseUntilMS, change.NowMS, before.StartedAtMS,
		before.DeadlineAtMS, change.NowMS, before.ImportID, before.ImportVersion, before.ReleaseYearMax,
		before.RootID, before.RootDigest, before.RelativePath, before.CreatedByUserID)
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}

func (records scanRecords) Clear(ctx context.Context, change application.ScanMutation) error {
	if err := records.fence(ctx, change); err != nil {
		return err
	}
	if err := clearUnpublishedScan(ctx, records.executor, change.Before.ImportID); err != nil {
		return err
	}
	result, err := records.executor.ExecContext(
		ctx,
		`UPDATE emulationstation_imports SET
phase='DISCOVERING_GAMELISTS',gamelist_count=0,invalid_gamelist_count=0,collection_count=0,folder_entry_count=0,
game_count=0,estimated_source_bytes=0,mapped_collection_count=0,skipped_collection_count=0,processable_item_count=0,
blocked_item_count=0,media_warning_count=0,discovered_cover_count=0,discovered_video_count=0,
version=version+1,updated_at_ms=? WHERE id=? AND version=? AND state='SCANNING'`,
		change.NowMS,
		change.Before.ImportID,
		change.Before.ImportVersion,
	)
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}
