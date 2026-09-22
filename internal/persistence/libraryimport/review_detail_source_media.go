package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	application "retrom/internal/service/libraryimport"
)

func (records ReviewMedia) SourceMedia(
	ctx context.Context,
	itemID string,
) (application.ReviewSourceMedia, bool, error) {
	var result application.ReviewSourceMedia
	err := records.executor.QueryRowContext(ctx, `
SELECT source.id,source.import_id,'SOURCE',COALESCE(collection.name,''),
COALESCE(json_extract(source.source_flags_json,'$.hidden'),0),
COALESCE(json_extract(source.source_flags_json,'$.adult'),0),
COALESCE(json_extract(source.source_flags_json,'$.kidGame'),0),
EXISTS(
 SELECT 1 FROM source_import_item_assets asset
 WHERE asset.item_id=source.id
 AND asset.kind='COVER' AND asset.state='COPIED'
AND asset.blob_id IS NOT NULL
),
(
 SELECT asset.width_px FROM source_import_item_assets asset
 WHERE asset.item_id=source.id
 AND asset.kind='COVER' AND asset.state='COPIED'
),
(
 SELECT asset.height_px FROM source_import_item_assets asset
 WHERE asset.item_id=source.id
 AND asset.kind='COVER' AND asset.state='COPIED'
),
EXISTS(
 SELECT 1 FROM source_import_item_assets asset
 WHERE asset.item_id=source.id
 AND asset.kind='VIDEO' AND asset.state='COPIED'
 AND asset.blob_id IS NOT NULL
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
		return application.ReviewSourceMedia{}, false, nil
	}
	if err != nil {
		return application.ReviewSourceMedia{}, false, fmt.Errorf("query review source media: %w", err)
	}
	return result, true, nil
}
