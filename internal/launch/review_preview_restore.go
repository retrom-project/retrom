package launch

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
)

type reviewPreviewRestore struct {
	blobID, format sql.NullString
}

// A trial restore is a new ordinary preview. Its payload and content are frozen
// in the same transaction, independently of later saves or closure of the source.
func loadReviewPreviewRestore(
	ctx context.Context, transaction *sql.Tx, request ReviewPreviewRequest,
	source reviewPreviewSource, content reviewPreviewContentSet, now int64,
) (reviewPreviewRestore, error) {
	if request.RestoreFromPreviewID == nil {
		return reviewPreviewRestore{}, nil
	}
	var restore reviewPreviewRestore
	err := transaction.QueryRowContext(ctx, `
SELECT preview.checkpoint_payload_blob_id,preview.checkpoint_format
FROM review_preview_sessions preview
JOIN blobs blob ON blob.id=preview.checkpoint_payload_blob_id
JOIN runtime_targets target ON target.provider_id=preview.provider_id AND target.target_id=preview.target_id
WHERE preview.id=? AND preview.actor_user_id=? AND preview.import_item_id=?
 AND preview.source_snapshot_id=? AND preview.provider_id=? AND preview.target_id=?
 AND preview.state IN ('ACTIVE','FINISHED') AND preview.hard_expires_at_ms>?
 AND preview.content_blob_id=? AND preview.content_logical_name=? AND preview.content_format=?
 AND preview.dependency_snapshot_json=? AND blob.size_bytes>0
 AND blob.size_bytes<=json_extract(target.checkpoint_json,'$.maxBytes')
 AND EXISTS(SELECT 1 FROM json_each(target.checkpoint_json,'$.readFormats') readable
  WHERE readable.type='text' AND readable.value=preview.checkpoint_format)
`, *request.RestoreFromPreviewID, request.ActorUserID, request.ImportItemID,
		source.SourceSnapshotID, source.ProviderID, source.TargetID, now,
		content.BlobID, content.LogicalName, content.Format, source.DependencySnapshot,
	).Scan(&restore.blobID, &restore.format)
	if err != nil {
		return reviewPreviewRestore{}, ErrSaveIncompatible
	}
	if err := matchReviewRestoreFiles(ctx, transaction, *request.RestoreFromPreviewID, content.Files); err != nil {
		return reviewPreviewRestore{}, err
	}
	return restore, nil
}

type reviewFileIdentity struct {
	role, name, virtualPath, blobID string
	sortOrder                       int
}

func matchReviewRestoreFiles(
	ctx context.Context, transaction *sql.Tx, previewID string, files []reviewPreviewFile,
) error {
	expected := make(map[reviewFileIdentity]bool, len(files))
	for _, file := range files {
		key := reviewFileIdentity{role: file.Role, name: file.LogicalName, blobID: file.BlobID, sortOrder: file.SortOrder}
		if file.VirtualPath != nil {
			key.virtualPath = *file.VirtualPath
		}
		expected[key] = true
	}
	rows, err := transaction.QueryContext(ctx, `
SELECT role,logical_name,COALESCE(virtual_path,''),blob_id,sort_order
FROM review_preview_files WHERE preview_session_id=?
`, previewID)
	if err != nil {
		return fmt.Errorf("read review restore files: %w", err)
	}
	defer func() { cleanup.Error("close review restore files", rows.Close()) }()
	for rows.Next() {
		var key reviewFileIdentity
		if err := rows.Scan(&key.role, &key.name, &key.virtualPath, &key.blobID, &key.sortOrder); err != nil {
			return fmt.Errorf("scan review restore file: %w", err)
		}
		if !expected[key] {
			return ErrSaveIncompatible
		}
		delete(expected, key)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate review restore files: %w", err)
	}
	if len(expected) != 0 {
		return ErrSaveIncompatible
	}
	return nil
}
