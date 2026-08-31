package payloadrelease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
)

func (service *Service) releaseImportItem(ctx context.Context, job claimedJob) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("payloadrelease/import item transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state, payloadState string
	var version int64
	var releaseJob sql.NullString
	err = transaction.QueryRowContext(ctx, `
SELECT state,version,payload_state,payload_release_job_id FROM import_items WHERE id=?
`, job.ScopeID).Scan(&state, &version, &payloadState, &releaseJob)
	if errors.Is(err, sql.ErrNoRows) {
		return releaseFailure("PAYLOAD_RELEASE_SCOPE_NOT_TERMINAL")
	}
	if err != nil {
		return fmt.Errorf("payloadrelease/read import item: %w", err)
	}
	if !terminalImportItem(state) || !releaseJob.Valid || releaseJob.String != job.ID {
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
UPDATE import_items SET payload_state='RELEASING',payload_last_error_code=NULL WHERE id=?
`, job.ScopeID); err != nil {
			return fmt.Errorf("payloadrelease/retry import item: %w", err)
		}
	}
	if err := service.releaseImportItemTx(
		ctx, transaction, job.ScopeID, reasonForImportState(state), service.now().UnixMilli(),
	); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("payloadrelease/import item commit: %w", err)
	}
	return nil
}

func (service *Service) releaseImportItemTx(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	reason Reason,
	now int64,
) error {
	blobs, err := importItemBlobIDs(ctx, transaction, itemID)
	if err != nil {
		return err
	}
	sessions, err := importItemConsumptionSessions(ctx, transaction, itemID)
	if err != nil {
		return err
	}
	if err := releaseImportReviewState(ctx, transaction, itemID, reason, now); err != nil {
		return err
	}
	for _, statement := range importItemDeleteStatements() {
		if err := execBatches(ctx, transaction, statement, itemID); err != nil {
			return err
		}
	}
	if err := service.releaseImportEvidence(ctx, transaction, itemID, now); err != nil {
		return err
	}
	if err := service.assertImportItemReleased(ctx, transaction, itemID); err != nil {
		return err
	}
	purged, err := service.purgeEligibleUploads(ctx, transaction, sessions, now)
	if err != nil {
		return err
	}
	blobs = append(blobs, purged...)
	if err := service.stageCandidates(ctx, transaction, blobs); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_items SET payload_state='RELEASED',payload_released_at_ms=?,payload_last_error_code=NULL
WHERE id=? AND payload_state IN ('RELEASING','FAILED','RELEASED')
`, now, itemID); err != nil {
		return fmt.Errorf("payloadrelease/complete import item: %w", err)
	}
	return nil
}

func releaseImportReviewState(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	reason Reason,
	now int64,
) error {
	if _, err := transaction.ExecContext(ctx, `
UPDATE upload_consumptions SET released_at_ms=?,release_reason=?,version=version+1
WHERE released_at_ms IS NULL AND (
  consumer_type='REVIEW_ASSET' AND consumer_id IN (SELECT id FROM review_uploaded_assets WHERE import_item_id=?) OR
  consumer_type='REVIEW_ARCADE_PARENT' AND consumer_id IN (
    SELECT id FROM review_arcade_parent_attachments WHERE import_item_id=?
  ) OR
  consumer_type='REVIEW_MULTI_DISC' AND consumer_id IN (
    SELECT id FROM review_multidisc_attachments WHERE import_item_id=?
  )
)
`, now, reason, itemID, itemID, itemID); err != nil {
		return fmt.Errorf("payloadrelease/release review consumptions: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE review_preview_sessions SET state='REVOKED',finished_at_ms=COALESCE(finished_at_ms,?),
updated_at_ms=?,version=version+1 WHERE import_item_id=? AND state IN ('CREATED','ACTIVE')
`, now, now, itemID); err != nil {
		return fmt.Errorf("payloadrelease/revoke review preview: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
DELETE FROM review_draft_screenshot_assets WHERE review_draft_id IN (
  SELECT id FROM review_drafts WHERE import_item_id=?
)
`, itemID); err != nil {
		return fmt.Errorf("payloadrelease/clear review screenshots: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE review_drafts SET cover_candidate_asset_id=NULL,background_candidate_asset_id=NULL,cover_uploaded_asset_id=NULL,
version=version+1,updated_at_ms=? WHERE import_item_id=?
`, now, itemID); err != nil {
		return fmt.Errorf("payloadrelease/clear review draft: %w", err)
	}
	return nil
}

func (service *Service) releaseImportEvidence(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	now int64,
) error {
	if _, err := transaction.ExecContext(ctx, `
UPDATE review_arcade_parent_attachments
SET accepted_blob_id=NULL,payload_released_at_ms=?,version=version+1,updated_at_ms=?
WHERE import_item_id=? AND accepted_blob_id IS NOT NULL
`, now, now, itemID); err != nil {
		return fmt.Errorf("payloadrelease/release parent evidence: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_item_multidisc_entries
SET state='PAYLOAD_RELEASED',upload_file_id=NULL,blob_id=NULL,payload_released_at_ms=?
WHERE blob_id IS NOT NULL AND source_snapshot_id IN (
  SELECT id FROM import_item_source_snapshots WHERE import_item_id=?
)
`, now, itemID); err != nil {
		return fmt.Errorf("payloadrelease/release multidisc evidence: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE content_hash_evidence
SET blob_id=NULL,archive_blob_id=NULL,archive_entry_ordinal=NULL,payload_released_at_ms=?
WHERE payload_released_at_ms IS NULL AND scrape_run_id IN (
  SELECT id FROM metadata_scrape_runs WHERE import_item_id=?
)
`, now, itemID); err != nil {
		return fmt.Errorf("payloadrelease/release hash evidence: %w", err)
	}
	if err := service.releaseLinkedPegasusItem(ctx, transaction, itemID, now); err != nil {
		return err
	}
	if err := service.releaseLinkedEmulationStationItem(ctx, transaction, itemID, now); err != nil {
		return err
	}
	return nil
}

func importItemDeleteStatements() []string {
	return []string{
		`DELETE FROM isolated_runtime_capabilities WHERE rowid IN (
 SELECT capability.rowid FROM isolated_runtime_capabilities capability
 JOIN review_preview_sessions preview ON preview.id=capability.preview_id
 WHERE preview.import_item_id=? AND preview.state IN ('EXPIRED','REVOKED')
 ORDER BY capability.rowid LIMIT 200
)`,
		`DELETE FROM isolated_runtime_bootstrap_tickets WHERE rowid IN (
 SELECT ticket.rowid FROM isolated_runtime_bootstrap_tickets ticket
 JOIN review_preview_sessions preview ON preview.id=ticket.preview_id
 WHERE preview.import_item_id=? AND preview.state IN ('EXPIRED','REVOKED')
 ORDER BY ticket.rowid LIMIT 200
)`,
		`DELETE FROM review_preview_files WHERE rowid IN (
 SELECT file.rowid FROM review_preview_files file
 JOIN review_preview_sessions preview ON preview.id=file.preview_session_id
 WHERE preview.import_item_id=? ORDER BY file.rowid LIMIT 200
)`,
		`DELETE FROM review_runtime_screenshots WHERE rowid IN (
 SELECT rowid FROM review_runtime_screenshots
 WHERE import_item_id=? ORDER BY rowid LIMIT 200
)`,
		`DELETE FROM review_preview_sessions WHERE rowid IN (
 SELECT rowid FROM review_preview_sessions WHERE import_item_id=? ORDER BY rowid LIMIT 200
)`,
		`DELETE FROM review_uploaded_assets WHERE rowid IN (
 SELECT rowid FROM review_uploaded_assets WHERE import_item_id=? ORDER BY rowid LIMIT 200
)`,
		`DELETE FROM scrape_candidate_assets WHERE rowid IN (
 SELECT asset.rowid FROM scrape_candidate_assets asset
 JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
 JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id
 WHERE run.import_item_id=? ORDER BY asset.rowid LIMIT 200
)`,
		`DELETE FROM import_item_validation_files WHERE rowid IN (
 SELECT file.rowid FROM import_item_validation_files file
 JOIN import_item_core_validations validation ON validation.id=file.import_item_core_validation_id
 WHERE validation.import_item_id=? ORDER BY file.rowid LIMIT 200
)`,
		`DELETE FROM import_item_source_snapshot_files WHERE rowid IN (
 SELECT file.rowid FROM import_item_source_snapshot_files file
 JOIN import_item_source_snapshots snapshot ON snapshot.id=file.source_snapshot_id
 WHERE snapshot.import_item_id=? ORDER BY file.rowid LIMIT 200
)`,
		`DELETE FROM import_item_source_files WHERE rowid IN (
 SELECT rowid FROM import_item_source_files WHERE import_item_id=? ORDER BY rowid LIMIT 200
)`,
	}
}
