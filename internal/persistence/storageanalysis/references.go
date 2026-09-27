package storageanalysis

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/service/storageanalysis"
)

func loadOwnership(
	ctx context.Context,
	tx dbapi.Tx,
) (map[string]struct{}, map[string]storageanalysis.Usage, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT file.id,file.owner_kind,EXISTS(SELECT 1 FROM game_assets asset WHERE asset.blob_id=file.id
AND asset.game_id=file.owner_id),file.retired_at_ms IS NULL FROM stored_files file`,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("read file ownership: %w", err)
	}
	defer func() { cleanup.Error("close file ownership", rows.Close()) }()
	retained := map[string]struct{}{}
	usages := map[string]storageanalysis.Usage{}
	for rows.Next() {
		var id, kind string
		var media bool
		var active bool
		if err := rows.Scan(&id, &kind, &media, &active); err != nil {
			return nil, nil, fmt.Errorf("scan file owner: %w", err)
		}
		if active {
			retained[id] = struct{}{}
		}
		usage := storageanalysis.UsageWorkflow
		switch kind {
		case "GAME":
			usage = storageanalysis.UsageGame
			if media {
				usage = storageanalysis.UsageMedia
			}
		case "SAVE_STATE":
			usage = storageanalysis.UsageSaves
		case "BIOS_INSTALLATION":
			usage = storageanalysis.UsageBIOS
		case "STAGING", "UPLOAD", "IMPORT_ITEM", "SOURCE_IMPORT_ITEM", "SCRAPE_RUN", "PROVIDER_RESPONSE":
		default:
			return nil, nil, fmt.Errorf("%w: unknown kind %q", storageanalysis.ErrOwnerInvalid, kind)
		}
		usages[id] = usage
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate file owners: %w", err)
	}
	return retained, usages, nil
}
