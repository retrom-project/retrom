package metadatascrape

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/service/metadatascrape"
)

func (repository *AssetRepository) Pending(ctx context.Context, runID string) ([]metadatascrape.PendingAsset, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT a.id,
a.provider_asset_id,
a.kind_hint,
a.ordinal,
a.source_path
FROM scrape_candidate_assets a
JOIN scrape_candidates c ON c.id=a.scrape_candidate_id
WHERE c.scrape_run_id=?
AND a.status='PENDING'
ORDER BY (SELECT count(*)
FROM scrape_candidate_hits h
WHERE h.scrape_candidate_id=c.id) DESC,
  (SELECT min(e.query_order)
FROM scrape_candidate_hits h
JOIN metadata_scrape_query_attempts q ON q.id=h.query_attempt_id
JOIN content_hash_evidence e ON e.id=q.content_hash_evidence_id
WHERE h.scrape_candidate_id=c.id),
  c.provider_game_id,
a.kind_hint,
a.ordinal,
a.id
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("metadatascrape/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	assets := make([]metadatascrape.PendingAsset, 0)
	for rows.Next() {
		var asset metadatascrape.PendingAsset
		if err := rows.Scan(
			&asset.ID,
			&asset.Reference.ProviderAssetID,
			&asset.Reference.Kind,
			&asset.Reference.Ordinal,
			&asset.Reference.Path,
		); err != nil {
			return nil, fmt.Errorf("metadatascrape/service: %w", err)
		}
		assets = append(assets, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metadatascrape/service: %w", err)
	}
	return assets, nil
}
