package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type reviewHistoryAssetSnapshot struct {
	SelectedAssets struct {
		CoverCandidateAssetID *string `json:"coverCandidateAssetId"`
		CoverUploadedAssetID  *string `json:"coverUploadedAssetId"`
	} `json:"selectedAssets"`
}

func selectedReviewHistoryCoverURL(beforeJSON string) (sql.NullString, error) {
	var snapshot reviewHistoryAssetSnapshot
	if err := json.Unmarshal([]byte(beforeJSON), &snapshot); err != nil {
		return sql.NullString{}, fmt.Errorf("decode review history asset snapshot: %w", err)
	}
	assetID := snapshot.SelectedAssets.CoverUploadedAssetID
	if assetID == nil || *assetID == "" {
		assetID = snapshot.SelectedAssets.CoverCandidateAssetID
	}
	if assetID == nil || *assetID == "" {
		return sql.NullString{}, nil
	}
	return sql.NullString{String: "/api/v1/admin/review-assets/" + *assetID, Valid: true}, nil
}

func (server *Server) reviewHistoryCoverURL(
	ctx context.Context,
	itemID string,
	beforeJSON string,
) (sql.NullString, error) {
	selected, err := selectedReviewHistoryCoverURL(beforeJSON)
	if err != nil || selected.Valid {
		return selected, err
	}
	var pegasusItemID string
	err = server.database.QueryRowContext(ctx, `
SELECT item.id
FROM pegasus_import_items item
JOIN pegasus_import_item_assets asset ON asset.item_id=item.id
WHERE item.library_import_item_id=?
  AND asset.kind='COVER' AND asset.state='COPIED' AND asset.blob_id IS NOT NULL
ORDER BY item.id
LIMIT 1
`, itemID).Scan(&pegasusItemID)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.NullString{}, nil
	}
	if err != nil {
		return sql.NullString{}, fmt.Errorf("query Pegasus review history cover: %w", err)
	}
	return sql.NullString{
		String: "/api/v1/admin/review-assets/" + pegasusItemID + "?kind=COVER",
		Valid:  true,
	}, nil
}
