package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/persistence/contentquery"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

type ReviewMedia struct{ executor dbapi.Executor }

func (records ReviewMedia) UploadedAssets(
	ctx context.Context,
	itemID string,
) ([]libraryservice.ReviewUploadedAsset, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT id,kind,width_px,height_px,media_type,created_at_ms
FROM review_uploaded_assets
WHERE import_item_id=?
ORDER BY created_at_ms,id
`, itemID)
	if err != nil {
		return nil, fmt.Errorf("query uploaded review assets: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]libraryservice.ReviewUploadedAsset, 0)
	for rows.Next() {
		var row libraryservice.ReviewUploadedAsset
		if err := rows.Scan(&row.ID, &row.Kind, &row.WidthPX, &row.HeightPX, &row.MediaType, &row.CreatedAtMS); err != nil {
			return nil, fmt.Errorf("scan uploaded review asset: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate uploaded review assets: %w", err)
	}
	return result, nil
}

func (records ReviewMedia) RuntimeScreenshot(
	ctx context.Context,
	itemID string,
) (libraryservice.ReviewRuntimeScreenshot, bool, error) {
	result := libraryservice.ReviewRuntimeScreenshot{}
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT screenshot.id,screenshot.provider_id,screenshot.target_id,
screenshot.width_px,screenshot.height_px,screenshot.captured_at_ms
FROM review_runtime_screenshots screenshot
JOIN import_items draft ON draft.id=screenshot.import_item_id
JOIN runtime_preview_sessions captured ON captured.id=screenshot.preview_session_id
 AND captured.target_platform_instance_id=draft.target_platform_instance_id
JOIN (`+contentquery.CurrentContentSQL+`) content ON content.import_item_id=draft.id
 AND content.provider_id=screenshot.provider_id AND content.target_id=screenshot.target_id
WHERE screenshot.import_item_id=? AND screenshot.source_snapshot_id=draft.effective_source_snapshot_id
`, itemID).Scan(
		&result.ID, &result.ProviderID, &result.TargetID, &result.WidthPX, &result.HeightPX, &result.CapturedAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return libraryservice.ReviewRuntimeScreenshot{}, false, nil
	}
	if err != nil {
		return libraryservice.ReviewRuntimeScreenshot{}, false, fmt.Errorf("query review runtime screenshot: %w", err)
	}
	return result, true, nil
}
