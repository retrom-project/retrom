package metadatascrape

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/model/metadatascrape"
)

type initialRecords struct{ transaction *sql.Tx }

func BindInitialReview(transaction *sql.Tx) metadatascrape.InitialReviewScope {
	records := initialRecords{transaction}
	return metadatascrape.InitialReviewScope{Read: records, Write: records}
}

func (records initialRecords) Import(ctx context.Context, id string) (metadatascrape.InitialImport, bool, error) {
	var item metadatascrape.InitialImport
	err := records.transaction.QueryRowContext(ctx, `SELECT i.id,i.import_job_id,i.state,j.running_item_count,
 j.failed_item_count,j.rejected_file_count,j.version FROM metadata_scrape_runs r
 JOIN import_items i ON i.id=r.import_item_id JOIN import_jobs j ON j.id=i.import_job_id WHERE r.id=?`, id).
		Scan(&item.ItemID, &item.ImportJobID, &item.ItemState, &item.Running, &item.Failed, &item.Rejected, &item.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return item, false, nil
	}
	if err != nil {
		return item, false, fmt.Errorf("query initial scrape scope: %w", err)
	}
	return item, true, nil
}

func (records initialRecords) Candidates(ctx context.Context, id string) ([]metadatascrape.InitialCandidate, error) {
	rows, err := records.transaction.QueryContext(ctx, `SELECT c.id,c.normalized_metadata_json,c.provider_game_id,
 (SELECT count(*) FROM scrape_candidate_hits h WHERE h.scrape_candidate_id=c.id),
 COALESCE((SELECT min(e.query_order) FROM scrape_candidate_hits h
 JOIN metadata_scrape_query_attempts q ON q.id=h.query_attempt_id
 JOIN content_hash_evidence e ON e.id=q.content_hash_evidence_id WHERE h.scrape_candidate_id=c.id),2147483647)
 FROM scrape_candidates c WHERE c.scrape_run_id=?`, id)
	if err != nil {
		return nil, fmt.Errorf("query initial scrape candidates: %w", err)
	}
	defer func() { cleanup.Error("close initial candidates", rows.Close()) }()
	candidates := make([]metadatascrape.InitialCandidate, 0)
	for rows.Next() {
		var candidate metadatascrape.InitialCandidate
		if err := rows.Scan(
			&candidate.ID,
			&candidate.MetadataJSON,
			&candidate.ProviderGameID,
			&candidate.HitCount,
			&candidate.FirstQueryOrder,
		); err != nil {
			return nil, fmt.Errorf("scan initial scrape candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate initial scrape candidates: %w", err)
	}
	return candidates, nil
}

func (records initialRecords) Draft(ctx context.Context, id string) (metadatascrape.InitialDraft, error) {
	var draft metadatascrape.InitialDraft
	err := records.transaction.QueryRowContext(
		ctx,
		`SELECT id,metadata_json FROM review_drafts WHERE import_item_id=?`,
		id,
	).Scan(
		&draft.ID,
		&draft.MetadataJSON,
	)
	if err != nil {
		return draft, fmt.Errorf("query initial review draft: %w", err)
	}
	return draft, nil
}

func (records initialRecords) ReadyAssets(ctx context.Context, id string) ([]metadatascrape.InitialAsset, error) {
	rows, err := records.transaction.QueryContext(ctx, `SELECT id,kind_hint,ordinal FROM scrape_candidate_assets
 WHERE scrape_candidate_id=? AND status='READY' ORDER BY ordinal,id`, id)
	if err != nil {
		return nil, fmt.Errorf("query initial ready assets: %w", err)
	}
	defer func() { cleanup.Error("close initial ready assets", rows.Close()) }()
	assets := make([]metadatascrape.InitialAsset, 0)
	for rows.Next() {
		var asset metadatascrape.InitialAsset
		if err := rows.Scan(&asset.ID, &asset.Kind, &asset.Ordinal); err != nil {
			return nil, fmt.Errorf("scan initial ready asset: %w", err)
		}
		assets = append(assets, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate initial ready assets: %w", err)
	}
	return assets, nil
}
