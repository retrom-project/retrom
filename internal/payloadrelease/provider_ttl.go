package payloadrelease

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
)

func (service *Service) releaseExpiredProviderPayloads(ctx context.Context) error {
	for {
		count, err := service.releaseExpiredProviderPayloadBatch(ctx)
		if err != nil || count < 200 {
			return err
		}
	}
}

func (service *Service) releaseExpiredProviderPayloadBatch(ctx context.Context) (int, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("payloadrelease/provider transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	responseIDs, err := collectIDs(ctx, transaction, `
SELECT response.id FROM metadata_provider_responses response
WHERE response.raw_payload_state='RETAINED' AND response.expires_at_ms<=?
AND NOT EXISTS(
  SELECT 1 FROM metadata_scrape_query_attempts attempt
  JOIN metadata_scrape_runs run ON run.id=attempt.scrape_run_id
  WHERE attempt.provider_response_id=response.id AND run.state='RUNNING'
)
ORDER BY response.expires_at_ms,response.id LIMIT 200
	`, now)
	if err != nil {
		return 0, err
	}
	var blobs []string
	for _, responseID := range responseIDs {
		ids, err := collectIDs(ctx, transaction, `
SELECT raw_response_blob_id FROM metadata_provider_responses WHERE id=?
`, responseID)
		if err != nil {
			return 0, err
		}
		blobs = append(blobs, ids...)
		if _, err := transaction.ExecContext(ctx, `
DELETE FROM metadata_provider_cache WHERE current_response_id=?
`, responseID); err != nil {
			return 0, fmt.Errorf("payloadrelease/provider cache release: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `
UPDATE metadata_provider_responses
SET raw_response_blob_id=NULL,raw_payload_state='RELEASED',raw_payload_released_at_ms=?
WHERE id=? AND raw_payload_state='RETAINED'
`, now, responseID); err != nil {
			return 0, fmt.Errorf("payloadrelease/provider release: %w", err)
		}
	}
	if err := service.stageCandidates(ctx, transaction, blobs); err != nil {
		return 0, err
	}
	if err := transaction.Commit(); err != nil {
		return 0, fmt.Errorf("payloadrelease/provider commit: %w", err)
	}
	return len(responseIDs), nil
}
