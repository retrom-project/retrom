package mediaaccess

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	service "retrom/internal/service/mediaaccess"
)

func (reader reader) Game(ctx context.Context, id string) (service.GameAsset, bool, error) {
	var asset service.GameAsset
	err := dbapi.QueryRowContext(ctx, reader.executor, `
SELECT blob.value,json_extract(blob.value, '$.sha256'),asset.media_type,game.status
FROM game_assets asset JOIN json_each(json_array(asset.file_record)) blob ON blob.value IS NOT NULL JOIN
games game ON game.id=asset.game_id
WHERE asset.id=?`, id).Scan(&asset.FileRecord, &asset.Digest, &asset.MediaType, &asset.GameState)
	if errors.Is(err, sql.ErrNoRows) {
		return asset, false, nil
	}
	if err != nil {
		return asset, false, fmt.Errorf("read game asset: %w", err)
	}
	return asset, true, nil
}

func (reader reader) Save(ctx context.Context, id string) (service.SaveScreenshot, bool, error) {
	var screenshot service.SaveScreenshot
	err := dbapi.QueryRowContext(ctx, reader.executor, `
SELECT blob.value,json_extract(blob.value, '$.sha256'),json_extract(blob.value, '$.media_type'),
save.profile_id,save.deleted_at_ms IS NOT NULL,game.status
FROM save_states save JOIN json_each(json_array(save.screenshot_file_record)) blob ON blob.value IS NOT
NULL JOIN games game ON game.id=save.game_id
WHERE save.id=?`, id).Scan(&screenshot.FileRecord, &screenshot.Digest, &screenshot.MediaType,
		&screenshot.ProfileID, &screenshot.Deleted, &screenshot.GameState)
	if errors.Is(err, sql.ErrNoRows) {
		return screenshot, false, nil
	}
	if err != nil {
		return screenshot, false, fmt.Errorf("read save screenshot: %w", err)
	}
	return screenshot, true, nil
}

func (reader reader) Review(ctx context.Context, id string) ([]service.ReviewAsset, error) {
	rows, err := reader.executor.QueryContext(ctx, reviewAssetsSQL, id, id, id)
	if err != nil {
		return nil, fmt.Errorf("read review assets: %w", err)
	}
	return readReviewAssets(rows)
}

func (reader reader) Sources(ctx context.Context, id, kind string) ([]service.ReviewAsset, error) {
	rows, err := reader.executor.QueryContext(ctx, sourceAssetsSQL, id, kind)
	if err != nil {
		return nil, fmt.Errorf("read source assets: %w", err)
	}
	return readReviewAssets(rows)
}

func readReviewAssets(rows dbapi.Rows) ([]service.ReviewAsset, error) {
	defer func() { cleanup.Error("close review media rows", rows.Close()) }()
	assets := []service.ReviewAsset{}
	for rows.Next() {
		var asset service.ReviewAsset
		if err := rows.Scan(&asset.FileRecord, &asset.Digest, &asset.MediaType, &asset.Kind, &asset.State,
			&asset.ItemState, &asset.GameState); err != nil {
			return nil, fmt.Errorf("scan review asset: %w", err)
		}
		assets = append(assets, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review assets: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close review media snapshot: %w", err)
	}
	return assets, nil
}
