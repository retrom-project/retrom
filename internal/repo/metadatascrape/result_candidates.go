package metadatascrape

import (
	"context"
	"fmt"

	"retrom/internal/model/metadatascrape"
	"retrom/internal/repo/recordstore"
)

func (records resultRecords) Candidate(
	ctx context.Context,
	value metadatascrape.CandidateRecord,
) (metadatascrape.CandidateIdentity, error) {
	result, err := records.transaction.ExecContext(ctx, `INSERT INTO scrape_candidates
 (id,scrape_run_id,primary_response_id,provider_game_id,normalized_metadata_json,evidence_json,created_at_ms)
 VALUES(?,?,?,?,?,?,?) ON CONFLICT(scrape_run_id,provider_game_id) DO NOTHING`,
		value.ID, value.RunID, value.ResponseID, value.ProviderGameID, value.MetadataJSON, value.EvidenceJSON, value.Now)
	if err != nil {
		return metadatascrape.CandidateIdentity{}, fmt.Errorf("insert scrape candidate: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return metadatascrape.CandidateIdentity{}, fmt.Errorf("count inserted candidate: %w", err)
	}
	candidate := metadatascrape.CandidateIdentity{ID: value.ID, Created: changed == 1}
	if candidate.Created {
		return candidate, nil
	}
	err = records.transaction.QueryRowContext(
		ctx,
		`SELECT id FROM scrape_candidates WHERE scrape_run_id=? AND provider_game_id=?`,
		value.RunID,
		value.ProviderGameID,
	).Scan(
		&candidate.ID,
	)
	if err != nil {
		return candidate, fmt.Errorf("query existing scrape candidate: %w", err)
	}
	return candidate, nil
}

func (records resultRecords) Hit(ctx context.Context, value metadatascrape.CandidateHit) error {
	_, err := records.transaction.ExecContext(
		ctx,
		`INSERT INTO scrape_candidate_hits
 (scrape_candidate_id,query_attempt_id,matched_hashes_json,created_at_ms) VALUES(?,?,?,?)`,
		value.CandidateID,
		value.AttemptID,
		value.HashesJSON,
		value.Now,
	)
	if err != nil {
		return fmt.Errorf("insert candidate evidence hit: %w", err)
	}
	return nil
}

func (records resultRecords) Assets(ctx context.Context, values []metadatascrape.CandidateAsset) error {
	for _, value := range values {
		_, err := recordstore.CreateScrapeCandidateAssets(ctx, records.transaction, `INSERT INTO scrape_candidate_assets
 (id,scrape_candidate_id,provider_response_id,provider_asset_id,kind_hint,ordinal,source_path,status,version,
 created_at_ms,updated_at_ms,media_fetch_job_id)
 VALUES(?,?,?,?,?,?,?,'PENDING',1,?,?,?)`,
			value.ID, value.CandidateID, value.ResponseID, value.Reference.ProviderAssetID,
			value.Reference.Kind, value.Reference.Ordinal, value.Reference.Path, value.Now, value.Now, value.MediaJobID)
		if err != nil {
			return fmt.Errorf("insert pending candidate asset: %w", err)
		}
	}
	return nil
}
