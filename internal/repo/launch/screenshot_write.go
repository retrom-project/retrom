package launch

import (
	"context"
	"fmt"

	blobmodel "retrom/internal/model/blob"

	application "retrom/internal/model/launch"
	"retrom/internal/repo/blobcatalog"
	"retrom/internal/repo/recordstore"
)

func (records screenshotRecords) Replace(ctx context.Context, plan application.ScreenshotWrite) error {
	image, source, now := plan.Image, plan.Source, plan.AtMS
	blobID, err := blobcatalog.EnsureRecord(ctx, records.executor, blobmodel.PreparedBlob{
		SHA256: image.SHA256, MD5: image.MD5, SHA1: image.SHA1, CRC32: image.CRC32, Size: image.SizeBytes,
	}, image.MediaType, now)
	if err != nil {
		return fmt.Errorf("register screenshot blob: %w", err)
	}
	// Retain only the current trial result for the item, including across validations.
	if _, err := records.executor.ExecContext(ctx, `DELETE FROM review_runtime_screenshots
WHERE import_item_id=? AND validation_id<>?`, source.ItemID, source.ValidationID); err != nil {
		return fmt.Errorf("delete prior screenshot: %w", err)
	}
	_, err = recordstore.CreateReviewRuntimeScreenshots(ctx, records.executor, `
INSERT INTO review_runtime_screenshots(id,import_item_id,preview_session_id,source_snapshot_id,
validation_id,provider_id,target_id,blob_id,media_type,width_px,height_px,
captured_at_ms,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(import_item_id,validation_id) DO UPDATE SET
id=excluded.id,preview_session_id=excluded.preview_session_id,source_snapshot_id=excluded.source_snapshot_id,
provider_id=excluded.provider_id,target_id=excluded.target_id,
blob_id=excluded.blob_id,media_type=excluded.media_type,
width_px=excluded.width_px,height_px=excluded.height_px,
captured_at_ms=excluded.captured_at_ms,updated_at_ms=excluded.updated_at_ms`,
		plan.ID, source.ItemID, source.PreviewID, source.SourceSnapshotID, source.ValidationID,
		source.ProviderID, source.TargetID, blobID, image.MediaType, image.WidthPX, image.HeightPX, now, now, now)
	if err != nil {
		return fmt.Errorf("write screenshot: %w", err)
	}
	return nil
}
