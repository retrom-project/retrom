package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
)

func copyCandidateReviewAsset(
	ctx context.Context,
	transaction *sql.Tx,
	itemID, gameID, assetID, kind string,
	ordinal int,
	now int64,
) error {
	var blobID, mediaType string
	var width, height int64
	err := transaction.QueryRowContext(ctx, `
SELECT a.blob_id,
a.width_px,
a.height_px,
a.media_type
FROM scrape_candidate_assets a
JOIN scrape_candidates c ON c.id=a.scrape_candidate_id
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
WHERE a.id=?
AND a.status='READY'
AND r.import_item_id=?
AND r.state='COMPLETED'
`, assetID, itemID).Scan(&blobID, &width, &height, &mediaType)
	if err != nil {
		return ErrInvalid
	}
	assetUUID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_assets(
id,game_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?)
`, assetUUID.String(), gameID, blobID, kind, ordinal, width, height, mediaType, now); err != nil {
		return fmt.Errorf("copy approved review asset: %w", err)
	}
	return nil
}

func copyUploadedReviewCover(
	ctx context.Context,
	transaction *sql.Tx,
	itemID, gameID, assetID string,
	now int64,
) error {
	var blobID, mediaType string
	var width, height int64
	if err := transaction.QueryRowContext(ctx, `
SELECT blob_id,width_px,height_px,media_type
FROM review_uploaded_assets
WHERE id=? AND import_item_id=? AND kind='COVER'
`, assetID, itemID).Scan(&blobID, &width, &height, &mediaType); err != nil {
		return ErrInvalid
	}
	assetUUID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_assets(
id,game_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms
) VALUES(?,?,?, 'COVER',0,?,?,?,?)
`, assetUUID.String(), gameID, blobID, width, height, mediaType, now); err != nil {
		return fmt.Errorf("copy uploaded review cover: %w", err)
	}
	return nil
}

func (service *Service) copyReviewAssets(
	ctx context.Context,
	transaction *sql.Tx,
	itemID, gameID string,
	coverID, uploadedCoverID, backgroundID sql.NullString,
	now int64,
) ([]string, error) {
	if coverID.Valid {
		if err := copyCandidateReviewAsset(
			ctx, transaction, itemID, gameID, coverID.String, "COVER", 0, now,
		); err != nil {
			return nil, err
		}
	}
	if uploadedCoverID.Valid {
		if err := copyUploadedReviewCover(
			ctx, transaction, itemID, gameID, uploadedCoverID.String, now,
		); err != nil {
			return nil, err
		}
	}
	if backgroundID.Valid {
		if err := copyCandidateReviewAsset(
			ctx, transaction, itemID, gameID, backgroundID.String, "BACKGROUND", 0, now,
		); err != nil {
			return nil, err
		}
	}
	rows, err := transaction.QueryContext(
		ctx,
		`
SELECT s.candidate_asset_id
FROM review_draft_screenshot_assets s
JOIN review_drafts d ON d.id=s.review_draft_id
WHERE d.import_item_id=?
ORDER BY s.ordinal
`,
		itemID,
	)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	screenshotIDs := make([]string, 0)
	for rows.Next() {
		var assetID string
		if err := rows.Scan(&assetID); err != nil {
			return nil, fmt.Errorf("libraryimport/service: %w", err)
		}
		screenshotIDs = append(screenshotIDs, assetID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("libraryimport/service: %w", err)
	}
	for ordinal, assetID := range screenshotIDs {
		if err := copyCandidateReviewAsset(
			ctx, transaction, itemID, gameID, assetID, "SCREENSHOT", ordinal, now,
		); err != nil {
			return nil, err
		}
	}
	return screenshotIDs, nil
}

func nullableInt(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}
