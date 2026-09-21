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
WITH source(source_kind,id,import_id,source_label) AS (
 SELECT 'PEGASUS',pegasus.id,pegasus.import_id,COALESCE(collection.name,'')
 FROM pegasus_import_items pegasus
 LEFT JOIN pegasus_import_collections collection ON collection.id=pegasus.collection_id
 WHERE pegasus.library_import_item_id=?
 UNION ALL
 SELECT 'EMULATIONSTATION',emulationstation.id,emulationstation.import_id,
        COALESCE(collection.display_name,'')
 FROM emulationstation_import_items emulationstation
 LEFT JOIN emulationstation_import_collections collection ON collection.id=emulationstation.collection_id
 WHERE emulationstation.library_import_item_id=?
), assets(source_kind,item_id,kind,state,blob_id,width_px,height_px) AS (
 SELECT 'PEGASUS',item_id,kind,state,blob_id,width_px,height_px
 FROM pegasus_import_item_assets
 UNION ALL
 SELECT 'EMULATIONSTATION',item_id,kind,state,blob_id,width_px,height_px
 FROM emulationstation_import_item_assets
)
SELECT source.id,source.import_id,source.source_kind,source.source_label,
EXISTS(
 SELECT 1 FROM assets asset
 WHERE asset.source_kind=source.source_kind AND asset.item_id=source.id
 AND asset.kind='COVER' AND asset.state='COPIED'
AND asset.blob_id IS NOT NULL
),
(
 SELECT asset.width_px FROM assets asset
 WHERE asset.source_kind=source.source_kind AND asset.item_id=source.id
 AND asset.kind='COVER' AND asset.state='COPIED'
),
(
 SELECT asset.height_px FROM assets asset
 WHERE asset.source_kind=source.source_kind AND asset.item_id=source.id
 AND asset.kind='COVER' AND asset.state='COPIED'
),
EXISTS(
 SELECT 1 FROM assets asset
 WHERE asset.source_kind=source.source_kind AND asset.item_id=source.id
 AND asset.kind='VIDEO' AND asset.state='COPIED'
 AND asset.blob_id IS NOT NULL
)
FROM source
`, itemID, itemID).Scan(
		&result.SourceRefID,
		&result.ImportID,
		&result.SourceKind,
		&result.Label,
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
