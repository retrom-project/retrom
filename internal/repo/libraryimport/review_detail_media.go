package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/dbexec"
	application "retrom/internal/service/libraryimport"
)

type ReviewMedia struct{ executor dbexec.Executor }

func (records ReviewMedia) UploadedAssets(
	ctx context.Context,
	itemID string,
) ([]application.ReviewUploadedAsset, error) {
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
	result := make([]application.ReviewUploadedAsset, 0)
	for rows.Next() {
		var row application.ReviewUploadedAsset
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
	itemID, validationID string,
) (application.ReviewRuntimeScreenshot, bool, error) {
	result := application.ReviewRuntimeScreenshot{ValidationID: validationID}
	err := records.executor.QueryRowContext(ctx, `
SELECT screenshot.id,screenshot.provider_id,screenshot.target_id,
screenshot.width_px,screenshot.height_px,screenshot.captured_at_ms
FROM review_runtime_screenshots screenshot
JOIN review_drafts draft ON draft.import_item_id=screenshot.import_item_id
WHERE screenshot.import_item_id=? AND screenshot.validation_id=?
AND screenshot.source_snapshot_id=draft.effective_source_snapshot_id
`, itemID, validationID).Scan(
		&result.ID, &result.ProviderID, &result.TargetID, &result.WidthPX, &result.HeightPX, &result.CapturedAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ReviewRuntimeScreenshot{}, false, nil
	}
	if err != nil {
		return application.ReviewRuntimeScreenshot{}, false, fmt.Errorf("query review runtime screenshot: %w", err)
	}
	return result, true, nil
}
