package importdiscard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/emulationstationimport"
	"retrom/internal/libraryimport"
	"retrom/internal/payloadrelease"
	"retrom/internal/pegasusimport"
)

var errReleaseFailed = errors.New("IMPORT_BATCH_DISCARD_RELEASE_FAILED")

func (service *Service) process(ctx context.Context, kind, id, userID string) (bool, error) {
	if kind != "IMPORT" {
		stopped, err := service.stopSource(ctx, kind, id, userID)
		if err != nil || !stopped {
			return false, err
		}
		if err := service.recoverSourceLinks(ctx, kind, id); err != nil {
			return false, err
		}
	}
	ids, err := service.importIDs(ctx, kind, id)
	if err != nil {
		return false, err
	}
	for _, importID := range ids {
		done, err := service.discardImport(ctx, importID)
		if err != nil || !done {
			return false, err
		}
	}
	if kind != "IMPORT" {
		if err := service.releaseUnusedUploads(ctx, kind, id); err != nil {
			return false, err
		}
		return service.discardSourceItems(ctx, kind, id)
	}
	return true, nil
}

func (service *Service) stopSource(ctx context.Context, kind, id, userID string) (bool, error) {
	table, err := batchTable(kind)
	if err != nil {
		return false, err
	}
	var state string
	var version int64
	if err := service.database.QueryRowContext(ctx, `
SELECT state,version FROM `+table+` WHERE id=?`,
		id).
		Scan(&state, &version); err != nil {
		return false, fmt.Errorf("importdiscard/read source execution: %w", err)
	}
	if state == "CANCEL_REQUESTED" {
		return false, nil
	}
	if state != "QUEUED" && state != "RUNNING" {
		return true, nil
	}
	if kind == "PEGASUS" {
		_, _, err = service.pegasus.Cancel(ctx, id, version, reason, userID)
	} else {
		_, _, err = service.emulationstation.Cancel(ctx, id, version, reason, userID)
	}
	if errors.Is(err, pegasusimport.ErrNotCancellable) || errors.Is(err, emulationstationimport.ErrNotCancellable) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("importdiscard/stop source: %w", err)
	}
	return false, nil
}

func (service *Service) importIDs(ctx context.Context, kind, id string) ([]string, error) {
	if kind == "IMPORT" {
		return []string{id}, nil
	}
	table, err := batchTable(kind)
	if err != nil {
		return nil, err
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT DISTINCT library_import_job_id FROM `+table[:len(table)-1]+`_items
WHERE import_id=? AND library_import_job_id IS NOT NULL
UNION SELECT job.id FROM server_import_upload_owners owner
JOIN import_jobs job ON job.upload_session_id=owner.upload_session_id
JOIN `+table[:len(table)-1]+`_items item ON item.id=owner.source_item_id
WHERE owner.kind=? AND item.import_id=? ORDER BY 1`, id, kind, id)
	if err != nil {
		return nil, fmt.Errorf("importdiscard/list child imports: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	var ids []string
	for rows.Next() {
		var item string
		if err := rows.Scan(&item); err != nil {
			return nil, fmt.Errorf("importdiscard/read child import: %w", err)
		}
		ids = append(ids, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("importdiscard/read child imports: %w", err)
	}
	return ids, nil
}

func (service *Service) discardImport(ctx context.Context, id string) (bool, error) {
	var state string
	var version int64
	if err := service.database.QueryRowContext(ctx, `
SELECT state,version FROM import_jobs WHERE id=?`,
		id).
		Scan(&state, &version); err != nil {
		return false, fmt.Errorf("importdiscard/read import: %w", err)
	}
	if state == "CANCEL_REQUESTED" {
		return false, nil
	}
	if state != "CANCELLED" && state != "COMPLETED" && state != "FAILED" {
		_, _, err := service.importer.CancelForDiscard(ctx, id, version)
		if errors.Is(err, libraryimport.ErrInvalid) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("importdiscard/stop import: %w", err)
		}
		return false, nil
	}
	done, err := service.importer.DiscardBatchReviews(ctx, id)
	if err != nil {
		return false, fmt.Errorf("importdiscard/discard reviews: %w", err)
	}
	if !done {
		return false, nil
	}
	if err := service.importer.ReleaseDiscardedBatch(ctx, id); err != nil {
		return false, fmt.Errorf("importdiscard/release import: %w", err)
	}
	return true, nil
}

func (service *Service) discardSourceItems(ctx context.Context, kind, id string) (bool, error) {
	table, err := batchTable(kind)
	if err != nil {
		return false, err
	}
	itemsTable := table[:len(table)-1] + "_items"
	tx, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("importdiscard/source transaction: %w", err)
	}
	defer cleanup.Rollback(tx)
	// Do not change a version already frozen by a release job.
	var releasing int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+itemsTable+` WHERE import_id=?
AND payload_state IN ('RELEASING','FAILED')
AND execution_state NOT IN ('PUBLISHED','SKIPPED_EXISTING','REVIEW_DISCARDED')`,
		id).
		Scan(&releasing); err != nil {
		return false, fmt.Errorf("importdiscard/check source releases: %w", err)
	}
	if releasing > 0 {
		var failed int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+itemsTable+` item
 JOIN jobs job ON job.id=item.payload_release_job_id WHERE item.import_id=? AND job.state='FAILED'`, id).
			Scan(&failed); err != nil {
			return false, fmt.Errorf("importdiscard/check failed releases: %w", err)
		}
		if failed > 0 {
			return false, errReleaseFailed
		}
		return false, nil
	}
	now := service.now().UnixMilli()
	skipped := ""
	if kind == "EMULATIONSTATION" {
		skipped = "skipped_mapping_item_count=0,"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE `+itemsTable+` SET execution_state='REVIEW_DISCARDED',retryable=0,
completed_at_ms=COALESCE(completed_at_ms,?),updated_at_ms=?,version=version+1
WHERE import_id=? AND execution_state NOT IN ('PUBLISHED','SKIPPED_EXISTING','REVIEW_DISCARDED')`,
		now, now, id); err != nil {
		return false, fmt.Errorf("importdiscard/discard source items: %w", err)
	}
	ids, err := payloadrelease.CollectScopeIDs(ctx, tx, `
SELECT id FROM `+itemsTable+` WHERE import_id=? AND payload_state='RETAINED'`, id)
	if err != nil {
		return false, fmt.Errorf("importdiscard/list source releases: %w", err)
	}
	for _, item := range ids {
		if err := scheduleSourceRelease(ctx, tx, kind, item, now); err != nil {
			return false, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE `+table+` SET `+skipped+`state='COMPLETED',phase=NULL,cancel_reason=NULL,retryable=0,
completed_at_ms=COALESCE(completed_at_ms,?),updated_at_ms=?,version=version+1,
review_pending_item_count=0,blocked_item_count=0,failed_item_count=0,cancelled_item_count=0,
review_discarded_item_count=(SELECT count(*) FROM `+itemsTable+`
 WHERE import_id=? AND execution_state='REVIEW_DISCARDED')
WHERE id=?`, now, now, id, id); err != nil {
		return false, fmt.Errorf("importdiscard/close source batch: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("importdiscard/commit source discard: %w", err)
	}
	return true, nil
}

func scheduleSourceRelease(ctx context.Context, tx *sql.Tx, kind, id string, now int64) error {
	var err error
	if kind == "PEGASUS" {
		_, err = payloadrelease.ScheduleTerminalPegasusItem(ctx, tx, id, now)
	} else {
		_, err = payloadrelease.ScheduleTerminalEmulationStationItem(ctx, tx, id, now)
	}
	if err != nil {
		return fmt.Errorf("importdiscard/schedule source release: %w", err)
	}
	return nil
}
