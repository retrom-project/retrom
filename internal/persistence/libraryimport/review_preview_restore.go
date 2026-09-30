package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/launch"
)

func (records reviewPreviewRecords) Restore(
	ctx context.Context,
	id string,
) (application.PreviewRestore, bool, error) {
	var restore application.PreviewRestore
	var formats string
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT preview.actor_user_id,review_binding.import_item_id,review_binding.source_snapshot_id,preview.provider_id,
 preview.target_id,preview.state,preview.hard_expires_at_ms,preview.content_file_record,
 preview.content_logical_name,preview.content_format,preview.dependency_snapshot_json,
 preview.checkpoint_payload_file_record,preview.checkpoint_format,json_extract(blob.value, '$.size_bytes'),
 COALESCE(json_extract(target.checkpoint_json,'$.maxBytes'),0),
 COALESCE(json_extract(target.checkpoint_json,'$.readFormats'),'[]')
FROM runtime_preview_sessions preview
JOIN review_preview_bindings review_binding ON review_binding.preview_session_id=preview.id
JOIN json_each(json_array(preview.checkpoint_payload_file_record))
blob ON blob.value IS NOT NULL
JOIN runtime_targets target ON target.provider_id=preview.provider_id AND target.target_id=preview.target_id
WHERE preview.id=?`, id).Scan(
		&restore.ActorID, &restore.ItemID, &restore.SnapshotID, &restore.ProviderID, &restore.TargetID, &restore.State,
		&restore.HardExpiresAtMS, &restore.ContentFileRecord, &restore.ContentName, &restore.ContentFormat,
		&restore.DependencySnapshot, &restore.FileRecord, &restore.Format, &restore.SizeBytes,
		&restore.MaximumBytes, &formats,
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
SELECT role,logical_name,file_record,virtual_path,sort_order
FROM runtime_preview_files WHERE preview_session_id=?`, id)
	if err != nil {
		return application.PreviewRestore{}, false, err
	}
	return restore, true, nil
}
