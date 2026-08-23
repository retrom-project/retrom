package payloadrelease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
)

type sourceImportItemSpec struct {
	label       string
	itemsTable  string
	filesTable  string
	assetsTable string
	scope       ScopeType
	reason      Reason
	terminal    func(string, bool) bool
}

var (
	pegasusSourceImportItem = sourceImportItemSpec{
		label:       "Pegasus",
		itemsTable:  "pegasus_import_items",
		filesTable:  "pegasus_import_item_files",
		assetsTable: "pegasus_import_item_assets",
		scope:       ScopePegasusImportItem,
		reason:      ReasonPegasusTerminal,
		terminal:    terminalPegasusItem,
	}
	emulationStationSourceImportItem = sourceImportItemSpec{
		label:       "EmulationStation",
		itemsTable:  "emulationstation_import_items",
		filesTable:  "emulationstation_import_item_files",
		assetsTable: "emulationstation_import_item_assets",
		scope:       ScopeEmulationStationImportItem,
		reason:      ReasonEmulationStationTerminal,
		terminal:    terminalEmulationStationItem,
	}
)

func (service *Service) releaseSourceImportItem(
	ctx context.Context,
	job claimedJob,
	spec sourceImportItemSpec,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("payloadrelease/%s transaction: %w", spec.label, err)
	}
	defer cleanup.Rollback(transaction)
	var state, payloadState string
	var version int64
	var retryable bool
	var publicItem, releaseJob sql.NullString
	err = transaction.QueryRowContext(ctx, fmt.Sprintf(`
SELECT execution_state,retryable,version,payload_state,library_import_item_id,payload_release_job_id
FROM %s WHERE id=?
`, spec.itemsTable), job.ScopeID).Scan(&state, &retryable, &version, &payloadState, &publicItem, &releaseJob)
	if errors.Is(err, sql.ErrNoRows) {
		return releaseFailure("PAYLOAD_RELEASE_SCOPE_NOT_TERMINAL")
	}
	if err != nil {
		return fmt.Errorf("payloadrelease/read %s item: %w", spec.label, err)
	}
	if publicItem.Valid || !spec.terminal(state, retryable) || !releaseJob.Valid || releaseJob.String != job.ID {
		return releaseFailure("PAYLOAD_RELEASE_SCOPE_NOT_TERMINAL")
	}
	if version != job.Input.Inputs.ScopeVersion && payloadState != "RELEASED" {
		return releaseFailure("PAYLOAD_RELEASE_SCOPE_VERSION_MISMATCH")
	}
	if payloadState == "RELEASED" {
		return nil
	}
	if err := retrySourceImportItem(ctx, transaction, job.ScopeID, payloadState, spec); err != nil {
		return err
	}
	if err := service.releaseSourceImportItemPayload(
		ctx, transaction, job.ScopeID, service.now().UnixMilli(), spec,
	); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("payloadrelease/%s commit: %w", spec.label, err)
	}
	return nil
}

func retrySourceImportItem(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	payloadState string,
	spec sourceImportItemSpec,
) error {
	if payloadState != "FAILED" {
		return nil
	}
	query := fmt.Sprintf(`
UPDATE %s SET payload_state='RELEASING',payload_last_error_code=NULL WHERE id=?
`, spec.itemsTable)
	if _, err := transaction.ExecContext(ctx, query, itemID); err != nil {
		return fmt.Errorf("payloadrelease/retry %s item: %w", spec.label, err)
	}
	return nil
}

func (service *Service) releaseLinkedSourceImportItems(
	ctx context.Context,
	transaction *sql.Tx,
	publicItemID string,
	now int64,
	spec sourceImportItemSpec,
) error {
	itemIDs, err := collectIDs(ctx, transaction, fmt.Sprintf(`
SELECT id FROM %s WHERE library_import_item_id=? ORDER BY id
`, spec.itemsTable), publicItemID)
	if err != nil {
		return err
	}
	for _, itemID := range itemIDs {
		if err := service.releaseLinkedSourceImportItem(ctx, transaction, publicItemID, itemID, now, spec); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) releaseLinkedSourceImportItem(
	ctx context.Context,
	transaction *sql.Tx,
	publicItemID string,
	itemID string,
	now int64,
	spec sourceImportItemSpec,
) error {
	var terminal int
	terminalQuery := fmt.Sprintf(`
SELECT count(*) FROM %s WHERE id=? AND execution_state IN ('PUBLISHED','REVIEW_DISCARDED')
`, spec.itemsTable)
	if err := transaction.QueryRowContext(ctx, terminalQuery, itemID).Scan(&terminal); err != nil || terminal != 1 {
		return releaseFailure("PAYLOAD_RELEASE_SOURCE_NOT_TERMINAL")
	}
	linkQuery := fmt.Sprintf(`
UPDATE %s
SET payload_state='RELEASING',payload_release_job_id=(SELECT payload_release_job_id FROM import_items WHERE id=?),
payload_released_at_ms=NULL,payload_last_error_code=NULL,version=version+1
WHERE id=? AND payload_state='RETAINED'
`, spec.itemsTable)
	if _, err := transaction.ExecContext(ctx, linkQuery, publicItemID, itemID); err != nil {
		return fmt.Errorf("payloadrelease/link %s item: %w", spec.label, err)
	}
	return service.releaseSourceImportItemPayload(ctx, transaction, itemID, now, spec)
}

func (service *Service) releaseSourceImportItemPayload(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	now int64,
	spec sourceImportItemSpec,
) error {
	blobs, err := collectSourceImportItemBlobs(ctx, transaction, itemID, spec)
	if err != nil {
		return err
	}
	if err := releaseSourceImportItemReferences(ctx, transaction, itemID, now, spec); err != nil {
		return err
	}
	if err := service.stageCandidates(ctx, transaction, blobs); err != nil {
		return err
	}
	query := fmt.Sprintf(`
UPDATE %s
SET payload_state='RELEASED',payload_released_at_ms=?,payload_last_error_code=NULL,version=version+1
WHERE id=? AND payload_state IN ('RELEASING','FAILED','RELEASED')
`, spec.itemsTable)
	if _, err := transaction.ExecContext(ctx, query, now, itemID); err != nil {
		return fmt.Errorf("payloadrelease/complete %s item: %w", spec.label, err)
	}
	return nil
}

func collectSourceImportItemBlobs(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	spec sourceImportItemSpec,
) ([]string, error) {
	query := fmt.Sprintf(`
SELECT blob_id FROM %s WHERE item_id=?
UNION ALL SELECT source_archive_blob_id FROM %s WHERE item_id=?
UNION ALL SELECT blob_id FROM %s WHERE item_id=?
`, spec.filesTable, spec.filesTable, spec.assetsTable)
	return collectIDs(ctx, transaction, query, itemID, itemID, itemID)
}

func releaseSourceImportItemReferences(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	now int64,
	spec sourceImportItemSpec,
) error {
	fileQuery := fmt.Sprintf(`
UPDATE %s
SET state='PAYLOAD_RELEASED',blob_id=NULL,source_archive_blob_id=NULL,
source_archive_entry_ordinal=NULL,payload_released_at_ms=?,updated_at_ms=?
WHERE item_id=? AND (blob_id IS NOT NULL OR source_archive_blob_id IS NOT NULL)
`, spec.filesTable)
	if _, err := transaction.ExecContext(ctx, fileQuery, now, now, itemID); err != nil {
		return fmt.Errorf("payloadrelease/release %s files: %w", spec.label, err)
	}
	assetQuery := fmt.Sprintf(`
UPDATE %s
SET state='PAYLOAD_RELEASED',blob_id=NULL,payload_released_at_ms=?,updated_at_ms=?
WHERE item_id=? AND blob_id IS NOT NULL
`, spec.assetsTable)
	if _, err := transaction.ExecContext(ctx, assetQuery, now, now, itemID); err != nil {
		return fmt.Errorf("payloadrelease/release %s assets: %w", spec.label, err)
	}
	return ensureNoSourceImportItemReferences(ctx, transaction, itemID, spec)
}

func ensureNoSourceImportItemReferences(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	spec sourceImportItemSpec,
) error {
	query := fmt.Sprintf(`
SELECT (SELECT count(*) FROM %s
        WHERE item_id=? AND (blob_id IS NOT NULL OR source_archive_blob_id IS NOT NULL))+
       (SELECT count(*) FROM %s WHERE item_id=? AND blob_id IS NOT NULL)
`, spec.filesTable, spec.assetsTable)
	var count int
	if err := transaction.QueryRowContext(ctx, query, itemID, itemID).Scan(&count); err != nil || count != 0 {
		return releaseFailure("PAYLOAD_RELEASE_REFERENCE_REMAINS")
	}
	return nil
}
