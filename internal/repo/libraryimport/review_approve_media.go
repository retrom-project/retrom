package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/dbexec"
)

func scanApprovalAsset(row dbexec.Scanner) (application.ApprovalExternalAsset, bool, error) {
	var asset application.ApprovalExternalAsset
	err := row.Scan(&asset.BlobID, &asset.WidthPX, &asset.HeightPX, &asset.MediaType)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ApprovalExternalAsset{}, false, nil
	}
	if err != nil {
		return application.ApprovalExternalAsset{}, false, fmt.Errorf("read approval asset: %w", err)
	}
	return asset, true, nil
}

func (records reviewApprovalRecords) Candidate(
	ctx context.Context, itemID, assetID string,
) (application.ApprovalExternalAsset, bool, error) {
	return scanApprovalAsset(records.transaction.QueryRowContext(ctx, `
SELECT a.blob_id,a.width_px,a.height_px,a.media_type
FROM scrape_candidate_assets a
JOIN scrape_candidates c ON c.id=a.scrape_candidate_id
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
WHERE a.id=? AND a.status='READY' AND r.import_item_id=? AND r.state='COMPLETED'`, assetID, itemID))
}

func (records reviewApprovalRecords) UploadedCover(
	ctx context.Context, itemID, assetID string,
) (application.ApprovalExternalAsset, bool, error) {
	return scanApprovalAsset(records.transaction.QueryRowContext(ctx, `
SELECT blob_id,width_px,height_px,media_type FROM review_uploaded_assets
WHERE id=? AND import_item_id=? AND kind='COVER'`, assetID, itemID))
}

func (records reviewApprovalRecords) Screenshots(ctx context.Context, itemID string) ([]string, error) {
	return (ReviewDrafts{executor: records.transaction}).ScreenshotIDs(ctx, itemID)
}

func (records reviewApprovalRecords) BlobExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	if err := records.transaction.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM blobs WHERE id=?)`,
		id).Scan(&exists); err != nil {
		return false, fmt.Errorf("read approved source blob: %w", err)
	}
	return exists, nil
}

func (records reviewApprovalRecords) Origin(
	ctx context.Context, itemID string,
) (application.ApprovalOrigin, bool, error) {
	var origin application.ApprovalOrigin
	err := records.transaction.QueryRowContext(ctx, `
SELECT source_ref_id,source_kind FROM (
 SELECT id AS source_ref_id,'SERVER_PEGASUS_IMPORT' AS source_kind FROM pegasus_import_items
 WHERE library_import_item_id=? AND execution_state='REVIEW_PENDING'
 UNION ALL
 SELECT id AS source_ref_id,'SERVER_EMULATIONSTATION_IMPORT' AS source_kind FROM emulationstation_import_items
 WHERE library_import_item_id=? AND execution_state='REVIEW_PENDING'
) ORDER BY source_kind LIMIT 1`, itemID, itemID).Scan(&origin.RefID, &origin.Kind)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ApprovalOrigin{}, false, nil
	}
	if err != nil {
		return application.ApprovalOrigin{}, false, fmt.Errorf("read approval source: %w", err)
	}
	assets, err := records.originAssets(ctx, origin)
	if err != nil {
		return application.ApprovalOrigin{}, false, err
	}
	origin.Assets = assets
	return origin, true, nil
}

func (records reviewApprovalRecords) originAssets(
	ctx context.Context, origin application.ApprovalOrigin,
) ([]application.ApprovalExternalAsset, error) {
	table := "pegasus_import_item_assets"
	if origin.Kind == "SERVER_EMULATIONSTATION_IMPORT" {
		table = "emulationstation_import_item_assets"
	}
	rows, err := records.transaction.QueryContext(ctx, `
SELECT kind,blob_id,media_type,width_px,height_px FROM `+table+`
WHERE item_id=? AND state='COPIED' AND blob_id IS NOT NULL AND media_type IS NOT NULL
ORDER BY CASE kind WHEN 'COVER' THEN 0 ELSE 1 END`, origin.RefID)
	if err != nil {
		return nil, fmt.Errorf("read approval source assets: %w", err)
	}
	defer func() { cleanup.Error("close approval source assets", rows.Close()) }()
	assets := make([]application.ApprovalExternalAsset, 0)
	for rows.Next() {
		var asset application.ApprovalExternalAsset
		if err := rows.Scan(&asset.Kind, &asset.BlobID, &asset.MediaType, &asset.WidthPX,
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
