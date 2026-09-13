package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/repo/recordstore"
	"retrom/internal/repo/sessionstore"
	application "retrom/internal/service/payloadrelease"
)

func (records expirationRecords) ReleaseProvider(
	ctx context.Context, before application.ProviderExpiration, now int64,
) error {
	result, err := records.executor.ExecContext(ctx, `DELETE FROM metadata_provider_cache WHERE current_response_id=?`,
		before.ID)
	if err := expirationWrite(result, err, before.CacheCount); err != nil {
		return fmt.Errorf("release expired provider cache: %w", err)
	}
	result, err = recordstore.UpdateMetadataProviderResponses(ctx, records.executor, recordstore.Update{
		Set: `raw_response_blob_id=NULL,raw_payload_state='RELEASED',raw_payload_released_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND raw_payload_state=? AND raw_response_blob_id=? AND expires_at_ms=? AND expires_at_ms<=?
AND ` + providerNoRunning,
			Args: []any{before.ID, before.State, before.BlobID, before.ExpiresMS, now},
		},
		Values: []any{now},
	})
	if err := expirationWrite(result, err, 1); err != nil {
		return fmt.Errorf("release expired provider response: %w", err)
	}
	return nil
}

func (records expirationRecords) ExpirePreview(ctx context.Context, change application.PreviewExpiry) error {
	before := change.Before
	result, err := sessionstore.ChangePreview(ctx, records.executor, recordstore.Update{
		Set: `state=?,finished_at_ms=COALESCE(finished_at_ms,?),updated_at_ms=?,version=version+1,
checkpoint_payload_blob_id=NULL,checkpoint_format=NULL,checkpoint_created_at_ms=NULL,
restore_payload_blob_id=NULL,restore_checkpoint_format=NULL`,
		Scope: recordstore.Scope{
			Where: `id=? AND state=? AND version=? AND bootstrap_expires_at_ms=? AND hard_expires_at_ms=?
AND (finished_at_ms=? OR (finished_at_ms IS NULL AND ? IS NULL))
AND COALESCE(checkpoint_payload_blob_id,'')=? AND COALESCE(restore_payload_blob_id,'')=? AND ` + previewExpiryDue,
			Args: []any{
				before.ID, before.State, before.Version, before.BootstrapExpiresMS, before.HardExpiresMS,
				before.FinishedMS, before.FinishedMS, before.CheckpointBlobID, before.RestoreBlobID, change.NowMS, change.NowMS,
			},
		},
		Values: []any{change.State, change.NowMS, change.NowMS},
	})
	if err := expirationWrite(result, err, 1); err != nil {
		return fmt.Errorf("update expired preview: %w", err)
	}
	return nil
}

func expirationWrite(result sql.Result, err error, expected int64) error {
	if err != nil {
		return fmt.Errorf("write expiration record: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count expiration writes: %w", err)
	}
	if count != expected {
		return application.ErrExpirationSnapshotChanged
	}
	return nil
}
