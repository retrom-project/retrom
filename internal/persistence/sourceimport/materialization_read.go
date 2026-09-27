package sourceimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/filestore"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/sourceimport"
)

func (records materialRecords) Source(
	ctx context.Context,
	key application.MaterialKey,
) (application.MaterialSnapshot, error) {
	owned, err := itemWorkRecords(records).Current(ctx, key.ItemID)
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
		return application.MaterialSnapshot{}, fmt.Errorf("read Source material source: %w", err)
	}
	if result.FileRecord != "" {
		file, err := filestore.ParseRecord(result.FileRecord)
		if err != nil {
			return application.MaterialSnapshot{}, fmt.Errorf("source: %w", err)
		}
		result.Blob = application.VerifiedBlob{
			ID: result.FileRecord, SHA256: file.SHA256,
			MD5: file.MD5, SHA1: file.SHA1, CRC32: file.CRC32, Size: file.Size,
		}
	}
	return result, nil
}

func (records materialRecords) file(ctx context.Context, result *application.MaterialSnapshot) error {
	source := &result.Source
	err := dbapi.QueryRowContext(
		ctx, records.tx, `SELECT relative_path,size_bytes,source_facts_digest,state,COALESCE(file_record,'')
FROM source_import_item_files WHERE item_id=? AND ordinal=?`, source.Key.ItemID, source.Key.Ordinal).Scan(
		&source.Path, &source.Size, &source.Facts, &result.State, &result.FileRecord)
	if err != nil {
		return fmt.Errorf("read source file: %w", err)
	}
	return nil
}

func (records materialRecords) asset(ctx context.Context, result *application.MaterialSnapshot) error {
	source := &result.Source
	var warnings string
	err := dbapi.QueryRowContext(
		ctx,
		records.tx,
		`SELECT asset.relative_path,asset.size_bytes,asset.source_facts_digest,
COALESCE(asset.media_type,''),asset.width_px,asset.height_px,asset.state,COALESCE(asset.file_record,
''),
COALESCE(asset.warning_code,''),item.warnings_json
FROM source_import_item_assets asset JOIN source_import_items item ON item.id=asset.item_id
WHERE asset.item_id=? AND asset.kind=?`,
		source.Key.ItemID,
		source.Key.Kind,
	).
		Scan(
			&source.Path,
			&source.Size,
			&source.Facts,
			&source.MediaType,
			&source.Width,
			&source.Height,
			&result.State,
			&result.FileRecord,
			&result.WarningCode,
			&warnings,
		)
	if err != nil {
		return fmt.Errorf("read source asset: %w", err)
	}
	if err := json.Unmarshal([]byte(warnings), &result.Warnings); err != nil {
		return fmt.Errorf("decode Source material warnings: %w", err)
	}
	if result.Warnings == nil {
		return application.ErrInvalid
	}
	return nil
}
