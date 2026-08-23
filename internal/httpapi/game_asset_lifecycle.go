package httpapi

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
)

// retireSupersededGameAssets keeps metadata history textual while making only
// the current metadata revision a durable owner of game media payload.
func (server *Server) retireSupersededGameAssets(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, currentMetadataID string,
) error {
	rows, err := transaction.QueryContext(ctx, `
SELECT DISTINCT blob_id
FROM game_assets
WHERE game_id=? AND metadata_revision_id<>?
ORDER BY blob_id
`, gameID, currentMetadataID)
	if err != nil {
		return fmt.Errorf("httpapi/list superseded game assets: %w", err)
	}
	defer func() { cleanup.Error("close superseded game assets", rows.Close()) }()
	blobIDs := make([]string, 0)
	for rows.Next() {
		var blobID string
		if err := rows.Scan(&blobID); err != nil {
			return fmt.Errorf("httpapi/scan superseded game asset: %w", err)
		}
		blobIDs = append(blobIDs, blobID)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("httpapi/iterate superseded game assets: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("httpapi/close superseded game assets: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
DELETE FROM game_assets
WHERE game_id=? AND metadata_revision_id<>?
`, gameID, currentMetadataID); err != nil {
		return fmt.Errorf("httpapi/delete superseded game assets: %w", err)
	}
	if err := server.payloadReleases.StageCandidates(ctx, transaction, blobIDs); err != nil {
		return fmt.Errorf("httpapi/stage superseded game assets: %w", err)
	}
	return nil
}
