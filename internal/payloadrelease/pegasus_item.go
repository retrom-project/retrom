package payloadrelease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
)

func (service *Service) releasePegasusItem(ctx context.Context, job claimedJob) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("payloadrelease/Pegasus transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state, payloadState string
	var version int64
	var retryable bool
	var publicItem, releaseJob sql.NullString
	err = transaction.QueryRowContext(ctx, `
SELECT execution_state,retryable,version,payload_state,library_import_item_id,payload_release_job_id
FROM pegasus_import_items WHERE id=?
`, job.ScopeID).Scan(&state, &retryable, &version, &payloadState, &publicItem, &releaseJob)
	if errors.Is(err, sql.ErrNoRows) {
		return releaseFailure("PAYLOAD_RELEASE_SCOPE_NOT_TERMINAL")
	}
	if err != nil {
		return fmt.Errorf("payloadrelease/read Pegasus item: %w", err)
	}
	if publicItem.Valid || !terminalPegasusItem(state, retryable) || !releaseJob.Valid || releaseJob.String != job.ID {
		return releaseFailure("PAYLOAD_RELEASE_SCOPE_NOT_TERMINAL")
	}
	if version != job.Input.Inputs.ScopeVersion && payloadState != "RELEASED" {
		return releaseFailure("PAYLOAD_RELEASE_SCOPE_VERSION_MISMATCH")
	}
	if payloadState == "RELEASED" {
		return nil
	}
	if payloadState == "FAILED" {
		if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_items SET payload_state='RELEASING',payload_last_error_code=NULL WHERE id=?
`, job.ScopeID); err != nil {
			return fmt.Errorf("payloadrelease/retry Pegasus item: %w", err)
		}
	}
	if err := service.releasePegasusPayload(ctx, transaction, job.ScopeID, service.now().UnixMilli()); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("payloadrelease/Pegasus commit: %w", err)
	}
	return nil
}

func (service *Service) releaseLinkedPegasusItem(
	ctx context.Context,
	transaction *sql.Tx,
	publicItemID string,
	now int64,
) error {
	itemIDs, err := collectIDs(ctx, transaction, `
SELECT id FROM pegasus_import_items WHERE library_import_item_id=? ORDER BY id
`, publicItemID)
	if err != nil {
		return err
	}
	for _, itemID := range itemIDs {
		var terminal int
		if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM pegasus_import_items
WHERE id=? AND execution_state IN ('PUBLISHED','REVIEW_DISCARDED')
`, itemID).Scan(&terminal); err != nil || terminal != 1 {
			return releaseFailure("PAYLOAD_RELEASE_SOURCE_NOT_TERMINAL")
		}
		if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_items
SET payload_state='RELEASING',payload_release_job_id=(SELECT payload_release_job_id FROM import_items WHERE id=?),
payload_released_at_ms=NULL,payload_last_error_code=NULL,version=version+1
WHERE id=? AND payload_state='RETAINED'
`, publicItemID, itemID); err != nil {
			return fmt.Errorf("payloadrelease/link Pegasus item: %w", err)
		}
		if err := service.releasePegasusPayload(ctx, transaction, itemID, now); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) releasePegasusPayload(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	now int64,
) error {
	blobs, err := collectIDs(ctx, transaction, `
SELECT blob_id FROM pegasus_import_item_files WHERE item_id=?
UNION ALL SELECT source_archive_blob_id FROM pegasus_import_item_files WHERE item_id=?
UNION ALL SELECT blob_id FROM pegasus_import_item_assets WHERE item_id=?
`, itemID, itemID, itemID)
	if err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_item_files
SET state='PAYLOAD_RELEASED',blob_id=NULL,source_archive_blob_id=NULL,source_archive_entry_ordinal=NULL,
payload_released_at_ms=?,updated_at_ms=?
WHERE item_id=? AND (blob_id IS NOT NULL OR source_archive_blob_id IS NOT NULL)
`, now, now, itemID); err != nil {
		return fmt.Errorf("payloadrelease/release Pegasus files: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_item_assets
SET state='PAYLOAD_RELEASED',blob_id=NULL,payload_released_at_ms=?,updated_at_ms=?
WHERE item_id=? AND blob_id IS NOT NULL
`, now, now, itemID); err != nil {
		return fmt.Errorf("payloadrelease/release Pegasus assets: %w", err)
	}
	var count int
	if err := transaction.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM pegasus_import_item_files
        WHERE item_id=? AND (blob_id IS NOT NULL OR source_archive_blob_id IS NOT NULL))+
       (SELECT count(*) FROM pegasus_import_item_assets WHERE item_id=? AND blob_id IS NOT NULL)
`, itemID, itemID).Scan(&count); err != nil || count != 0 {
		return releaseFailure("PAYLOAD_RELEASE_REFERENCE_REMAINS")
	}
	if err := service.stageCandidates(ctx, transaction, blobs); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_items SET payload_state='RELEASED',payload_released_at_ms=?,payload_last_error_code=NULL,
version=version+1 WHERE id=? AND payload_state IN ('RELEASING','FAILED','RELEASED')
`, now, itemID); err != nil {
		return fmt.Errorf("payloadrelease/complete Pegasus item: %w", err)
	}
	return nil
}
