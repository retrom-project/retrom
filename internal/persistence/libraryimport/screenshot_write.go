package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/filestore"
	"retrom/internal/persistence/recordstore"
	libraryservice "retrom/internal/service/libraryimport"
)

func (records screenshotRecords) Replace(ctx context.Context, plan libraryservice.ScreenshotWrite) error {
	image, source, now := plan.Image, plan.Source, plan.AtMS
	fileRecord, err := filestore.FileRecord(filestore.Metadata{
		Record: image.FileRecord, Path: image.StoragePath,
		SHA256: image.SHA256,
		MD5:    image.MD5,
		SHA1:   image.SHA1,
		CRC32:  image.CRC32,
		Size:   image.SizeBytes,
	}, image.MediaType)
	if err != nil {
		return fmt.Errorf("encode screenshot file: %w", err)
	}

	_, err = recordstore.UpsertReviewRuntimeScreenshots(
		ctx,
		records.executor,
		`
INSERT INTO review_runtime_screenshots(id,import_item_id,preview_session_id,source_snapshot_id,
provider_id,target_id,file_record,media_type,width_px,height_px,
captured_at_ms,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(import_item_id) DO UPDATE SET
id=excluded.id,preview_session_id=excluded.preview_session_id,source_snapshot_id=excluded.source_snapshot_id,
provider_id=excluded.provider_id,target_id=excluded.target_id,
file_record=excluded.file_record,media_type=excluded.media_type,
width_px=excluded.width_px,height_px=excluded.height_px,
captured_at_ms=excluded.captured_at_ms,updated_at_ms=excluded.updated_at_ms`,
		plan.ID,
		source.ItemID,
		source.PreviewID,
		source.SourceSnapshotID,
		source.ProviderID,
		source.TargetID,
		fileRecord,
		image.MediaType,
		image.WidthPX,
		image.HeightPX,
		now,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("write screenshot: %w", err)
	}
	return nil
}
