package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
)

func (service *Service) purgeEligibleUploads(
	ctx context.Context,
	transaction *sql.Tx,
	sessionIDs []string,
	now int64,
) ([]string, error) {
	var released []string
	for _, sessionID := range uniqueStrings(sessionIDs) {
		for {
			ids, err := collectIDs(ctx, transaction, `
SELECT final_blob_id FROM upload_files file
WHERE file.upload_session_id=? AND file.state='COMPLETE'
AND NOT EXISTS(
  SELECT 1 FROM upload_consumptions consumption
  WHERE consumption.upload_session_id=file.upload_session_id
    AND (consumption.upload_file_id IS NULL OR consumption.upload_file_id=file.id)
    AND consumption.released_at_ms IS NULL
)
AND NOT EXISTS(SELECT 1 FROM import_item_source_files WHERE upload_file_id=file.id)
AND NOT EXISTS(SELECT 1 FROM import_item_source_snapshot_files WHERE upload_file_id=file.id)
AND NOT EXISTS(SELECT 1 FROM import_item_multidisc_entries WHERE upload_file_id=file.id)
AND NOT EXISTS(SELECT 1 FROM review_uploaded_assets WHERE upload_file_id=file.id)
AND NOT EXISTS(SELECT 1 FROM review_arcade_parent_attachments WHERE upload_file_id=file.id)
AND EXISTS(SELECT 1 FROM upload_sessions session WHERE session.id=file.upload_session_id
  AND session.state IN ('COMPLETE','FAILED','CANCELLED','EXPIRED'))
ORDER BY file.id LIMIT 200
`, sessionID)
			if err != nil {
				return nil, err
			}
			if len(ids) == 0 {
				break
			}
			if _, err := transaction.ExecContext(ctx, `
UPDATE upload_files SET state='PURGED',final_blob_id=NULL,payload_released_at_ms=?,updated_at_ms=?
WHERE id IN (
  SELECT file.id FROM upload_files file
  WHERE file.upload_session_id=? AND file.state='COMPLETE'
  AND NOT EXISTS(
    SELECT 1 FROM upload_consumptions consumption
    WHERE consumption.upload_session_id=file.upload_session_id
      AND (consumption.upload_file_id IS NULL OR consumption.upload_file_id=file.id)
      AND consumption.released_at_ms IS NULL
  )
  AND NOT EXISTS(SELECT 1 FROM import_item_source_files WHERE upload_file_id=file.id)
  AND NOT EXISTS(SELECT 1 FROM import_item_source_snapshot_files WHERE upload_file_id=file.id)
  AND NOT EXISTS(SELECT 1 FROM import_item_multidisc_entries WHERE upload_file_id=file.id)
  AND NOT EXISTS(SELECT 1 FROM review_uploaded_assets WHERE upload_file_id=file.id)
  AND NOT EXISTS(SELECT 1 FROM review_arcade_parent_attachments WHERE upload_file_id=file.id)
  ORDER BY file.id LIMIT 200
)
`, now, now, sessionID); err != nil {
				return nil, fmt.Errorf("payloadrelease/purge upload files: %w", err)
			}
			released = append(released, ids...)
			if len(ids) < 200 {
				break
			}
		}
	}
	return released, nil
}

func (service *Service) releaseConsumption(ctx context.Context, job claimedJob) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("payloadrelease/consumption transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var version int64
	var released sql.NullInt64
	var sessionID string
	err = transaction.QueryRowContext(ctx, `
SELECT version,released_at_ms,upload_session_id FROM upload_consumptions WHERE id=?
`, job.ScopeID).Scan(&version, &released, &sessionID)
	if err != nil {
		return releaseFailure("PAYLOAD_RELEASE_SCOPE_NOT_TERMINAL")
	}
	if version != job.Input.Inputs.ScopeVersion && !released.Valid {
		return releaseFailure("PAYLOAD_RELEASE_SCOPE_VERSION_MISMATCH")
	}
	now := service.now().UnixMilli()
	if !released.Valid {
		if _, err := transaction.ExecContext(ctx, `
UPDATE upload_consumptions SET released_at_ms=?,release_reason=?,version=version+1 WHERE id=? AND released_at_ms IS NULL
`, now, job.Input.Inputs.Reason, job.ScopeID); err != nil {
			return fmt.Errorf("payloadrelease/consumption update: %w", err)
		}
	}
	blobs, err := service.purgeEligibleUploads(ctx, transaction, []string{sessionID}, now)
	if err != nil {
		return err
	}
	if err := service.stageCandidates(ctx, transaction, blobs); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("payloadrelease/consumption commit: %w", err)
	}
	return nil
}
