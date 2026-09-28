package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

func scanApprovalAsset(row dbapi.Scanner) (libraryservice.ApprovalExternalAsset, bool, error) {
	var asset libraryservice.ApprovalExternalAsset
	err := row.Scan(&asset.FileRecord, &asset.WidthPX, &asset.HeightPX, &asset.MediaType)
	if errors.Is(err, sql.ErrNoRows) {
		return libraryservice.ApprovalExternalAsset{}, false, nil
	}
	if err != nil {
		return libraryservice.ApprovalExternalAsset{}, false, fmt.Errorf("read approval asset: %w", err)
	}
	return asset, true, nil
}

func (records reviewApprovalRecords) Candidate(
	ctx context.Context, itemID, assetID string,
) (libraryservice.ApprovalExternalAsset, bool, error) {
	return scanApprovalAsset(dbapi.QueryRowContext(ctx, records.transaction, `
SELECT a.file_record,a.width_px,a.height_px,a.media_type
FROM scrape_candidate_assets a
JOIN scrape_candidates c ON c.id=a.scrape_candidate_id
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
WHERE a.id=? AND a.status='READY' AND r.import_item_id=? AND r.state='COMPLETED'`, assetID, itemID))
}

func (records reviewApprovalRecords) UploadedCover(
	ctx context.Context, itemID, assetID string,
) (libraryservice.ApprovalExternalAsset, bool, error) {
	return scanApprovalAsset(dbapi.QueryRowContext(ctx, records.transaction, `
SELECT file_record,width_px,height_px,media_type FROM review_uploaded_assets
WHERE id=? AND import_item_id=? AND kind='COVER'`, assetID, itemID))
}

func (records reviewApprovalRecords) Screenshots(ctx context.Context, itemID string) ([]string, error) {
	return (ReviewDrafts{executor: records.transaction}).ScreenshotIDs(ctx, itemID)
}

func (records reviewApprovalRecords) Origin(
	ctx context.Context, itemID string,
) (libraryservice.ApprovalOrigin, bool, error) {
	var origin libraryservice.ApprovalOrigin
	err := dbapi.QueryRowContext(ctx, records.transaction, `
SELECT source_ref_id,source_kind FROM (
 SELECT id AS source_ref_id,'IMPORT_RECEIVE' AS source_kind FROM source_import_items
 WHERE library_import_item_id=? AND execution_state='REVIEW_PENDING'
) ORDER BY source_kind LIMIT 1`, itemID).Scan(&origin.RefID, &origin.Kind)
	if errors.Is(err, sql.ErrNoRows) {
		return libraryservice.ApprovalOrigin{}, false, nil
	}
	if err != nil {
		return libraryservice.ApprovalOrigin{}, false, fmt.Errorf("read approval source: %w", err)
	}
	assets, err := records.originAssets(ctx, itemID)
	if err != nil {
		return libraryservice.ApprovalOrigin{}, false, err
	}
	origin.Assets = assets
	return origin, true, nil
}

func (records reviewApprovalRecords) originAssets(
	ctx context.Context, itemID string,
) ([]libraryservice.ApprovalExternalAsset, error) {
	table := "import_item_assets"
	rows, err := records.transaction.QueryContext(ctx, `
SELECT kind,file_record,media_type,width_px,height_px FROM `+table+`
WHERE import_item_id=?
ORDER BY CASE kind WHEN 'COVER' THEN 0 ELSE 1 END`, itemID)
	if err != nil {
		return nil, fmt.Errorf("read approval source assets: %w", err)
	}
	defer func() { cleanup.Error("close approval source assets", rows.Close()) }()
	assets := make([]libraryservice.ApprovalExternalAsset, 0)
	for rows.Next() {
		var asset libraryservice.ApprovalExternalAsset
		if err := rows.Scan(&asset.Kind, &asset.FileRecord, &asset.MediaType, &asset.WidthPX,
			&asset.HeightPX); err != nil {
			return nil, fmt.Errorf("scan approval source asset: %w", err)
		}
		assets = append(assets, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate approval source assets: %w", err)
	}
	return assets, nil
}
