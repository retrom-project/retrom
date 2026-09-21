package mediaaccess

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	service "retrom/internal/service/mediaaccess"
)

func (reader reader) Game(ctx context.Context, id string) (service.GameAsset, bool, error) {
	var asset service.GameAsset
	err := reader.executor.QueryRowContext(ctx, `
SELECT blob.sha256,asset.media_type,game.status
FROM game_assets asset JOIN blobs blob ON blob.id=asset.blob_id JOIN games game ON game.id=asset.game_id
WHERE asset.id=?`, id).Scan(&asset.Digest, &asset.MediaType, &asset.GameState)
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
	err := reader.executor.QueryRowContext(ctx, `
SELECT blob.sha256,blob.media_type,save.profile_id,save.deleted_at_ms IS NOT NULL,game.status
FROM save_states save JOIN blobs blob ON blob.id=save.screenshot_blob_id JOIN games game ON game.id=save.game_id
WHERE save.id=?`, id).Scan(&screenshot.Digest, &screenshot.MediaType,
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
	rows, err := reader.executor.QueryContext(ctx, sourceAssetsSQL, id, kind, id, kind)
	if err != nil {
		return nil, fmt.Errorf("read source assets: %w", err)
	}
	return readReviewAssets(rows)
}

func readReviewAssets(rows *sql.Rows) ([]service.ReviewAsset, error) {
	defer func() { cleanup.Error("close review media rows", rows.Close()) }()
	assets := []service.ReviewAsset{}
	for rows.Next() {
		var asset service.ReviewAsset
		if err := rows.Scan(&asset.Digest, &asset.MediaType, &asset.Kind, &asset.State,
			&asset.ItemState, &asset.GameState, &asset.TerminalReview); err != nil {
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
