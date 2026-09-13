package pegasusimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/persistence/dbexec"
	application "retrom/internal/service/pegasusimport"
)

type ScanPublication struct{ database *sql.DB }

func NewScanPublication(database *sql.DB) *ScanPublication {
	return &ScanPublication{database: database}
}

func (repository *ScanPublication) WithScan(ctx context.Context, work func(application.ScanScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Pegasus scan publication: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := scanRecords{tx: tx}
	if err := work(application.ScanScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Pegasus scan publication: %w", err)
	}
	return nil
}

type scanRecords struct{ tx *sql.Tx }

func (records scanRecords) Current(ctx context.Context, jobID string) (application.ExecutionSnapshot, error) {
	return leaseRecords(records).Current(ctx, jobID)
}

const scanOwnerFence = ` WHERE id=? AND version=? AND state=? AND state='RUNNING'
AND kind=? AND kind='SERVER_PEGASUS_SCAN'
AND scope_type='PEGASUS_IMPORT' AND scope_id=? AND worker_id=? AND execution_no=? AND attempt_count=?
AND leased_until_ms=? AND leased_until_ms>? AND execution_deadline_at_ms=? AND execution_deadline_at_ms>?
AND EXISTS(SELECT 1 FROM pegasus_imports plan WHERE plan.id=jobs.scope_id AND plan.scan_job_id=jobs.id
AND plan.version=? AND plan.state=? AND plan.state='SCANNING' AND plan.import_job_id IS NULL
AND plan.scan_completed_at_ms IS NULL)`

func scanOwnerArgs(owner application.ScanLease) []any {
	b := owner.Before
	return []any{
		b.JobID, b.JobVersion, b.JobState, b.Kind, b.ImportID, b.WorkerID, b.ExecutionNo, b.Attempt,
		b.LeaseUntilMS, owner.NowMS, b.DeadlineMS, owner.NowMS, b.ImportVersion, b.ImportState,
	}
}

func (records scanRecords) guard(ctx context.Context, owner application.ScanLease) error {
	result, err := records.tx.ExecContext(ctx, `UPDATE jobs SET version=version`+scanOwnerFence, scanOwnerArgs(owner)...)
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}

func (records scanRecords) Shape(ctx context.Context, importID string) (application.ScanShape, error) {
	var result application.ScanShape
	err := records.tx.QueryRowContext(ctx, `SELECT
(SELECT count(*) FROM pegasus_import_metadata_files WHERE import_id=?),
(SELECT count(*) FROM pegasus_import_metadata_files WHERE import_id=? AND parse_state='INVALID'),
(SELECT count(*) FROM pegasus_import_collections WHERE import_id=?),
(SELECT count(*) FROM pegasus_import_items WHERE import_id=?),
(SELECT count(*) FROM pegasus_import_items WHERE import_id=? AND discovery_state<>'READY'),
(SELECT count(*) FROM pegasus_import_item_assets a JOIN pegasus_import_items i ON i.id=a.item_id
 WHERE i.import_id=? AND a.kind='COVER'),
(SELECT count(*) FROM pegasus_import_item_assets a JOIN pegasus_import_items i ON i.id=a.item_id
 WHERE i.import_id=? AND a.kind='VIDEO'),
(SELECT COALESCE(sum(f.size_bytes),0) FROM pegasus_import_item_files f
 JOIN pegasus_import_items i ON i.id=f.item_id WHERE i.import_id=?)+
(SELECT COALESCE(sum(a.size_bytes),0) FROM pegasus_import_item_assets a
 JOIN pegasus_import_items i ON i.id=a.item_id WHERE i.import_id=?)`,
		importID, importID, importID, importID, importID, importID, importID, importID, importID).Scan(
		&result.Metadata, &result.InvalidMetadata, &result.Collections, &result.Items, &result.Blocked,
		&result.Covers, &result.Videos, &result.EstimatedBytes)
	if err != nil {
		return application.ScanShape{}, fmt.Errorf("query persisted Pegasus scan shape: %w", err)
	}
	return result, nil
}
