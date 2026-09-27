package sourceimport

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/sourceimport"
)

func readReviewMedia(ctx context.Context, tx dbapi.Executor, sourceID string) ([]application.ReviewMedia, error) {
	rows, err := tx.QueryContext(ctx, `SELECT kind,file_record,media_type,width_px,height_px FROM source_import_item_assets
 WHERE item_id=? AND state='COPIED' AND file_record IS NOT NULL ORDER BY kind`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("read review media: %w", err)
	}
	defer func() { cleanup.Error("close Source media", rows.Close()) }()
	var result []application.ReviewMedia
	for rows.Next() {
		var media application.ReviewMedia
		if err := rows.Scan(&media.Kind, &media.File, &media.MediaType, &media.Width, &media.Height); err != nil {
			return nil, fmt.Errorf("read review media: %w", err)
		}
		result = append(result, media)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review media: %w", err)
	}
	return result, nil
}

func TransferReviewMedia(ctx context.Context, tx dbapi.Executor, itemID string,
	media []application.ReviewMedia, now int64,
) error {
	for _, asset := range media {
		if _, err := recordstore.InsertRows(ctx, tx, "import_item_assets", `
INSERT INTO import_item_assets(import_item_id,kind,file_record,media_type,width_px,height_px,created_at_ms)
 VALUES(?,?,?,?,?,?,?)`, itemID, asset.Kind, asset.File, asset.MediaType, asset.Width, asset.Height, now); err != nil {
			return fmt.Errorf("transfer review media: %w", err)
		}
	}
	return nil
}
