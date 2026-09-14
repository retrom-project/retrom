package metadatascrape

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/model/metadatascrape"
	"retrom/internal/repo/dbexec"
)

func (records mediaRecords) Ordering(ctx context.Context, id string) ([]metadatascrape.MediaOrder, error) {
	rows, err := records.executor.QueryContext(ctx, `SELECT a.id,c.provider_game_id,a.kind_hint,a.ordinal,
 (SELECT count(*) FROM scrape_candidate_hits h WHERE h.scrape_candidate_id=c.id),
 COALESCE((SELECT min(e.query_order) FROM scrape_candidate_hits h
 JOIN metadata_scrape_query_attempts q ON q.id=h.query_attempt_id
 JOIN content_hash_evidence e ON e.id=q.content_hash_evidence_id WHERE h.scrape_candidate_id=c.id),2147483647)
 FROM scrape_candidate_assets a JOIN scrape_candidates c ON c.id=a.scrape_candidate_id
 WHERE c.scrape_run_id=? AND a.media_fetch_job_id IS NOT NULL`, id)
	if err != nil {
		return nil, fmt.Errorf("query media order: %w", err)
	}
	defer func() { cleanup.Error("close media ordering", rows.Close()) }()
	assets := make([]metadatascrape.MediaOrder, 0)
	for rows.Next() {
		asset, err := scanMediaOrder(rows)
		if err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate media order: %w", err)
	}
	return assets, nil
}

func (records mediaRecords) Freeze(
	ctx context.Context, id string, assets []metadatascrape.MediaOrder, now int64,
) error {
	for order, asset := range assets {
		if err := mediaChanged(records.executor.ExecContext(ctx, `
UPDATE scrape_candidate_assets SET media_fetch_order=?,
 version=version+1,updated_at_ms=? WHERE id=? AND media_fetch_order IS NULL
 AND EXISTS(SELECT 1 FROM scrape_candidates c WHERE c.id=scrape_candidate_assets.scrape_candidate_id
 AND c.scrape_run_id=?)`, order, now, asset.ID, id)); err != nil {
			return err
		}
	}
	return mediaChanged(records.executor.ExecContext(ctx, `
UPDATE metadata_media_runs SET order_frozen_at_ms=?,
 version=version+1,updated_at_ms=? WHERE scrape_run_id=? AND order_frozen_at_ms IS NULL
 AND EXISTS(SELECT 1 FROM metadata_scrape_runs r WHERE r.id=scrape_run_id AND r.state<>'RUNNING')`, now, now, id))
}

func scanMediaOrder(row dbexec.Scanner) (metadatascrape.MediaOrder, error) {
	var asset metadatascrape.MediaOrder
	if err := row.Scan(&asset.ID, &asset.GameID, &asset.Kind, &asset.Ordinal, &asset.Hits, &asset.QueryOrder); err != nil {
		return asset, fmt.Errorf("scan media ranking facts: %w", err)
	}
	return asset, nil
}
