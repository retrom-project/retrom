package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

func (records ReviewMedia) SourceMedia(
	ctx context.Context,
	itemID string,
) (libraryservice.ReviewSourceMedia, bool, error) {
	var result libraryservice.ReviewSourceMedia
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT source.id,source.import_id,'SOURCE',COALESCE(collection.name,''),
COALESCE(json_extract(source.source_flags_json,'$.hidden'),0),
COALESCE(json_extract(source.source_flags_json,'$.adult'),0),
COALESCE(json_extract(source.source_flags_json,'$.kidGame'),0),
EXISTS(
 SELECT 1 FROM import_item_assets asset
 WHERE asset.import_item_id=source.library_import_item_id
 AND asset.kind='COVER'
AND asset.file_record IS NOT NULL
),
(
 SELECT asset.width_px FROM import_item_assets asset
 WHERE asset.import_item_id=source.library_import_item_id
 AND asset.kind='COVER'
),
(
 SELECT asset.height_px FROM import_item_assets asset
 WHERE asset.import_item_id=source.library_import_item_id
 AND asset.kind='COVER'
),
EXISTS(
 SELECT 1 FROM import_item_assets asset
 WHERE asset.import_item_id=source.library_import_item_id
 AND asset.kind='VIDEO'
 AND asset.file_record IS NOT NULL
)
FROM source_import_items source
LEFT JOIN source_import_collections collection ON collection.id=source.collection_id
WHERE source.library_import_item_id=?
`, itemID).Scan(
		&result.SourceRefID,
		&result.ImportID,
		&result.SourceKind,
		&result.Label,
		&result.SourceFlags.Hidden, &result.SourceFlags.Adult, &result.SourceFlags.KidGame,
		&result.HasCover,
		&result.CoverWidthPX,
		&result.CoverHeightPX,
		&result.HasVideo)
	if errors.Is(err, sql.ErrNoRows) {
		return libraryservice.ReviewSourceMedia{}, false, nil
	}
	if err != nil {
		return libraryservice.ReviewSourceMedia{}, false, fmt.Errorf("query review source media: %w", err)
	}
	return result, true, nil
}
