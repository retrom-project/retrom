package payloadrelease

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
)

func (records effectRecords) clearGameEvidence(ctx context.Context, gameID string, now int64) error {
	if err := records.checkedUpdate(
		ctx,
		"content_hash_evidence",
		recordstore.UpdateContentHashEvidence,
		recordstore.Update{
			Set: `blob_id=NULL,archive_blob_id=NULL,archive_entry_ordinal=NULL,payload_released_at_ms=?`,
			Scope: recordstore.Scope{
				Where: `
payload_released_at_ms IS NULL AND scrape_run_id IN (
  SELECT id FROM metadata_scrape_runs WHERE game_id=?
)
`,

				Args: []any{gameID},
			},
			Values: []any{now},
		},
	); err != nil {
		return fmt.Errorf("payloadrelease/release game evidence: %w", err)
	}
	return nil
}
