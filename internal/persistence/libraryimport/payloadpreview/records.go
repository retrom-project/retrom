package payloadpreview

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/sessionstore"
	application "retrom/internal/service/payloadrelease"
)

type Records struct{ Executor dbapi.Executor }

const previewExpiryDue = `(state='CREATED' AND bootstrap_expires_at_ms<=? OR hard_expires_at_ms<=? OR state='REVOKED')
AND (state NOT IN ('EXPIRED','REVOKED') OR checkpoint_payload_blob_id IS NOT NULL
OR restore_payload_blob_id IS NOT NULL)`

func (records Records) Previews(
	ctx context.Context, now int64, limit int,
) ([]application.PreviewExpiration, error) {
	rows, err := records.Executor.QueryContext(ctx, `SELECT id,state,version,bootstrap_expires_at_ms,hard_expires_at_ms,
finished_at_ms,COALESCE(checkpoint_payload_blob_id,''),COALESCE(restore_payload_blob_id,'')
FROM review_preview_sessions WHERE `+previewExpiryDue+` ORDER BY hard_expires_at_ms,id LIMIT ?`, now, now, limit)
	if err != nil {
		return nil, fmt.Errorf("select expired previews: %w", err)
	}
	defer func() { cleanup.Error("close expired previews", rows.Close()) }()
	var facts []application.PreviewExpiration
	for rows.Next() {
		var row application.PreviewExpiration
		if err := rows.Scan(&row.ID, &row.State, &row.Version, &row.BootstrapExpiresMS, &row.HardExpiresMS,
			&row.FinishedMS, &row.CheckpointBlobID, &row.RestoreBlobID); err != nil {
			return nil, fmt.Errorf("scan expired preview: %w", err)
		}
		facts = append(facts, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired previews: %w", err)
	}
	return facts, nil
}

func (records Records) ExpirePreview(ctx context.Context, change application.PreviewExpiry) error {
	before := change.Before
	result, err := sessionstore.ChangePreview(ctx, records.Executor, recordstore.Update{
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
