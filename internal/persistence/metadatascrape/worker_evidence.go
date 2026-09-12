package metadatascrape

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/service/metadatascrape"
)

func (repository *WorkerRepository) Evidence(
	ctx context.Context,
	runID string,
) ([]metadatascrape.WorkerEvidence, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT id,crc32,md5,sha1,sha256 FROM content_hash_evidence
WHERE scrape_run_id=? ORDER BY query_order,id LIMIT 8
`, runID)
	if err != nil {
		return nil, fmt.Errorf("metadatascrape/service: %w", err)
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
			&value.Hashes.SHA256,
		); err != nil {
			return nil, fmt.Errorf("metadatascrape/service: %w", err)
		}
		evidenceList = append(evidenceList, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metadatascrape/service: %w", err)
	}
	return evidenceList, nil
}
