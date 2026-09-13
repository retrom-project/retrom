package emulationstationimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	application "retrom/internal/service/emulationstationimport"
)

func (records materialRecords) Source(
	ctx context.Context,
	key application.MaterialKey,
) (application.MaterialSnapshot, error) {
	owned, err := itemWorkRecords(records).Item(ctx, key.ItemID)
	if err != nil {
		return application.MaterialSnapshot{}, err
	}
	result := application.MaterialSnapshot{Before: owned, Source: application.MaterialSource{Key: key}}
	if key.Kind == "" {
		err = records.file(ctx, &result)
	} else {
		err = records.asset(ctx, &result)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return application.MaterialSnapshot{}, application.ErrVersionConflict
	}
	if err != nil {
		return application.MaterialSnapshot{}, fmt.Errorf("read EmulationStation material source: %w", err)
	}
	if result.BlobID != "" {
		if err := records.executor.QueryRowContext(ctx,
			`SELECT sha256,md5,sha1,crc32,size_bytes FROM blobs WHERE id=?`, result.BlobID).Scan(
			&result.Blob.SHA256, &result.Blob.MD5, &result.Blob.SHA1, &result.Blob.CRC32, &result.Blob.Size); err != nil {
			return application.MaterialSnapshot{}, fmt.Errorf("read EmulationStation material blob: %w", err)
		}
	}
	return result, nil
}

func (records materialRecords) file(ctx context.Context, result *application.MaterialSnapshot) error {
	source := &result.Source
	err := records.executor.QueryRowContext(
		ctx,
		`SELECT relative_path,size_bytes,source_facts_digest,state,COALESCE(blob_id,'')
FROM emulationstation_import_item_files WHERE item_id=? AND ordinal=?`,
		source.Key.ItemID,
		source.Key.Ordinal,
	).Scan(
		&source.Path, &source.Size, &source.Facts, &result.State, &result.BlobID)
	if err != nil {
		return fmt.Errorf("read source file: %w", err)
	}
	return nil
}

func (records materialRecords) asset(ctx context.Context, result *application.MaterialSnapshot) error {
	source := &result.Source
	var warnings string
	err := records.executor.QueryRowContext(ctx, `SELECT asset.relative_path,asset.size_bytes,asset.source_facts_digest,
COALESCE(asset.media_type,''),asset.width_px,asset.height_px,asset.state,COALESCE(asset.blob_id,''),
COALESCE(asset.warning_code,''),item.warnings_json
FROM emulationstation_import_item_assets asset JOIN emulationstation_import_items item ON
item.id=asset.item_id
WHERE asset.item_id=? AND asset.kind=?`, source.Key.ItemID, source.Key.Kind).Scan(
		&source.Path,
		&source.Size,
		&source.Facts,
		&source.MediaType,
		&source.Width,
		&source.Height,
		&result.State,
		&result.BlobID,
		&result.WarningCode,
		&warnings,
	)
	if err != nil {
		return fmt.Errorf("read source asset: %w", err)
	}
	if err := json.Unmarshal([]byte(warnings), &result.Warnings); err != nil {
		return fmt.Errorf("decode EmulationStation material warnings: %w", err)
	}
	if result.Warnings == nil {
		return application.ErrInvalid
	}
	return nil
}
