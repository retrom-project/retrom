package metadatascrape

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/service/metadatascrape"
)

func (repository *WorkerRepository) Evidence(
	ctx context.Context,
	runID string,
) (metadatascrape.EvidenceProgress, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT e.id,e.crc32,e.md5,e.sha1,e.sha256,COALESCE(a.attempt_no,0),COALESCE(p.outcome,''),COALESCE(a.source,'')
 FROM content_hash_evidence e LEFT JOIN metadata_scrape_query_attempts a ON a.content_hash_evidence_id=e.id
 AND a.attempt_no=(SELECT max(q.attempt_no) FROM metadata_scrape_query_attempts q WHERE q.content_hash_evidence_id=e.id)
 LEFT JOIN metadata_provider_responses p ON p.id=a.provider_response_id
 WHERE e.scrape_run_id=? ORDER BY e.query_order,e.id LIMIT 8
`, runID)
	if err != nil {
		return metadatascrape.EvidenceProgress{}, fmt.Errorf("metadatascrape/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	evidenceList := make([]metadatascrape.WorkerEvidence, 0, 8)
	for rows.Next() {
		var value metadatascrape.WorkerEvidence
		if err := rows.Scan(
			&value.ID,
			&value.Hashes.CRC32,
			&value.Hashes.MD5,
			&value.Hashes.SHA1,
			&value.Hashes.SHA256, &value.Attempts, &value.LastOutcome, &value.LastSource,
		); err != nil {
			return metadatascrape.EvidenceProgress{}, fmt.Errorf("metadatascrape/service: %w", err)
		}
		evidenceList = append(evidenceList, value)
	}
	if err := rows.Err(); err != nil {
		return metadatascrape.EvidenceProgress{}, fmt.Errorf("metadatascrape/service: %w", err)
	}
	if err := rows.Close(); err != nil {
		return metadatascrape.EvidenceProgress{}, fmt.Errorf("close evidence progress: %w", err)
	}
	progress := metadatascrape.EvidenceProgress{Items: evidenceList}
	err = repository.database.QueryRowContext(ctx,
		`SELECT count(*) FROM scrape_candidates WHERE scrape_run_id=?`, runID).Scan(&progress.CandidateCount)
	if err != nil {
		return progress, fmt.Errorf("count persisted metadata candidates: %w", err)
	}
	return progress, nil
}
