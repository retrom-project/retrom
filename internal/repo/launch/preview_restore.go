package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	application "retrom/internal/service/launch"
)

func (records previewCreationRecords) Restore(
	ctx context.Context,
	id string,
) (application.PreviewRestore, bool, error) {
	var restore application.PreviewRestore
	var formats string
	err := records.executor.QueryRowContext(ctx, `
SELECT preview.actor_user_id,preview.import_item_id,preview.source_snapshot_id,preview.provider_id,
 preview.target_id,preview.state,preview.hard_expires_at_ms,preview.content_blob_id,
 preview.content_logical_name,preview.content_format,preview.dependency_snapshot_json,
 preview.checkpoint_payload_blob_id,preview.checkpoint_format,blob.size_bytes,
 COALESCE(json_extract(target.checkpoint_json,'$.maxBytes'),0),
 COALESCE(json_extract(target.checkpoint_json,'$.readFormats'),'[]')
FROM review_preview_sessions preview JOIN blobs blob ON blob.id=preview.checkpoint_payload_blob_id
JOIN runtime_targets target ON target.provider_id=preview.provider_id AND target.target_id=preview.target_id
WHERE preview.id=?`, id).Scan(
		&restore.ActorID, &restore.ItemID, &restore.SnapshotID, &restore.ProviderID, &restore.TargetID, &restore.State,
		&restore.HardExpiresAtMS, &restore.ContentBlobID, &restore.ContentName, &restore.ContentFormat,
		&restore.DependencySnapshot, &restore.BlobID, &restore.Format, &restore.SizeBytes, &restore.MaximumBytes, &formats,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.PreviewRestore{}, false, nil
	}
	if err != nil {
		return application.PreviewRestore{}, false, fmt.Errorf("query preview restore: %w", err)
	}
	if err := json.Unmarshal([]byte(formats), &restore.ReadFormats); err != nil {
		return application.PreviewRestore{}, false, fmt.Errorf("decode preview restore formats: %w", err)
	}
	restore.Files, err = previewCreationFiles(ctx, records.executor, `
SELECT role,logical_name,blob_id,virtual_path,sort_order
FROM review_preview_files WHERE preview_session_id=?`, id)
	if err != nil {
		return application.PreviewRestore{}, false, err
	}
	return restore, true, nil
}
