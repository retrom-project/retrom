package metadatascrape

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/metadatascrape"
	"retrom/internal/repo/dbexec"
)

type EvidenceQueries struct{ executor dbexec.Executor }

func BindEvidenceQueries(executor dbexec.Executor) *EvidenceQueries {
	return &EvidenceQueries{executor: executor}
}

func (records *EvidenceQueries) ReviewCandidates(
	ctx context.Context,
	itemID string,
) ([]application.ReviewCandidateRecord, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT c.id,c.scrape_run_id,c.provider_game_id,c.normalized_metadata_json,c.evidence_json,c.created_at_ms
FROM scrape_candidates c JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
WHERE r.import_item_id=? AND r.state='COMPLETED' ORDER BY r.created_at_ms DESC,c.created_at_ms,c.id`, itemID)
	if err != nil {
		return nil, fmt.Errorf("query review candidates: %w", err)
	}
	defer func() { cleanup.Error("close review candidates", rows.Close()) }()
	result := make([]application.ReviewCandidateRecord, 0)
	for rows.Next() {
		var row application.ReviewCandidateRecord
		if err := rows.Scan(&row.ID,
			&row.RunID,
			&row.ProviderGameID,
			&row.MetadataJSON,
			&row.EvidenceJSON,
			&row.CreatedAtMS); err != nil {
			return nil, fmt.Errorf("scan review candidate: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review candidates: %w", err)
	}
	return result, nil
}

func (records *EvidenceQueries) CandidateAssets(
	ctx context.Context,
	ids []string,
) ([]application.CandidateAssetView, error) {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, fmt.Errorf("encode candidate IDs: %w", err)
	}
	rows, err := records.executor.QueryContext(ctx, `
SELECT scrape_candidate_id,id,provider_asset_id,kind_hint,ordinal,status,width_px,height_px,media_type,error_code
FROM scrape_candidate_assets WHERE scrape_candidate_id IN(SELECT value FROM json_each(?))
ORDER BY scrape_candidate_id,kind_hint,ordinal,id`, string(encoded))
	if err != nil {
		return nil, fmt.Errorf("query candidate assets: %w", err)
	}
	defer func() { cleanup.Error("close candidate assets", rows.Close()) }()
	result := make([]application.CandidateAssetView, 0)
	for rows.Next() {
		var row application.CandidateAssetView
		if err := rows.Scan(&row.CandidateID,
			&row.ID,
			&row.ProviderAssetID,
			&row.Kind,
			&row.Ordinal,
			&row.Status,
			&row.WidthPX,
			&row.HeightPX,
			&row.MediaType,
			&row.ErrorCode); err != nil {
			return nil, fmt.Errorf("scan candidate asset: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate candidate assets: %w", err)
	}
	return result, nil
}
