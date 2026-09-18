package emulationstationimport

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
)

type ScanPublication struct {
	database      *sql.DB
	preCommitHook func(dbexec.Executor) error
}

func NewScanPublication(database *sql.DB) *ScanPublication {
	return &ScanPublication{database: database}
}

func (repository *ScanPublication) WithPreCommitHook(hook func(dbexec.Executor) error) {
	repository.preCommitHook = hook
}

func (repository *ScanPublication) LoadScanOwner(
	ctx context.Context, jobID string,
) (application.LeaseSnapshot, bool, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.LeaseSnapshot{}, false,
			fmt.Errorf("begin EmulationStation scan read: %w", err)
	}
	defer dbexec.Rollback(tx)
	snapshot, found, err := leaseRecords{executor: tx}.Current(ctx, jobID)
	if err != nil {
		return application.LeaseSnapshot{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return application.LeaseSnapshot{}, false,
			fmt.Errorf("commit EmulationStation scan read: %w", err)
	}
	return snapshot, found, nil
}

func (repository *ScanPublication) commitScan(
	ctx context.Context, fn func(scanRecords) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin EmulationStation scan transaction: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := fn(scanRecords{executor: tx}); err != nil {
		return err
	}
	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(tx); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit EmulationStation scan transaction: %w", err)
	}
	return nil
}

func (repository *ScanPublication) CommitScanClear(
	ctx context.Context, change application.ScanMutation,
) error {
	return repository.commitScan(ctx, func(r scanRecords) error {
		return r.Clear(ctx, change)
	})
}

func (repository *ScanPublication) CommitScanHeaders(
	ctx context.Context, change application.ScanMutation, proj application.ScanProjection,
) error {
	return repository.commitScan(ctx, func(r scanRecords) error {
		return r.Headers(ctx, change, proj)
	})
}

func (repository *ScanPublication) CommitScanItems(
	ctx context.Context, change application.ScanMutation, items []application.ScanItem,
) error {
	return repository.commitScan(ctx, func(r scanRecords) error {
		return r.Items(ctx, change, items)
	})
}

func (repository *ScanPublication) CommitScanComplete(
	ctx context.Context, change application.ScanMutation, proj application.ScanProjection,
) error {
	return repository.commitScan(ctx, func(r scanRecords) error {
		return r.Complete(ctx, change, proj)
	})
}

func (repository *ScanPublication) CommitScanRejection(
	ctx context.Context, change application.ScanMutation, proj application.ScanProjection,
) error {
	return repository.commitScan(ctx, func(r scanRecords) error {
		if err := r.Headers(ctx, change, proj); err != nil {
			return fmt.Errorf("persist rejected scan headers: %w", err)
		}
		return r.Reject(ctx, change, proj)
	})
}

type scanRecords struct{ executor dbexec.Executor }

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
