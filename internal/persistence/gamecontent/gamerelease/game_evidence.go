package gamerelease

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/filedeletion"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/releaseops"
)

func (records Records) ClearEvidence(ctx context.Context, gameID string, now int64) error {
	paths, err := dbapi.QueryStrings(ctx, records.Executor, `SELECT 'saves/'||id FROM save_states WHERE game_id=?1
 UNION SELECT 'scrapes/'||id FROM metadata_scrape_runs WHERE game_id=?1`, gameID)
	if err != nil {
		return fmt.Errorf("clear evidence: %w", err)
	}
	for _, path := range paths {
		if err := filedeletion.QueuePath(ctx, records.Executor, path, now); err != nil {
			return fmt.Errorf("clear evidence: %w", err)
		}
	}

	if err := (releaseops.Records{Executor: records.Executor}).CheckedUpdate(
		ctx,
		"content_hash_evidence",
		recordstore.UpdateContentHashEvidence,
		recordstore.Update{
			Set: `file_record=NULL,archive_file_record=NULL,archive_entry_ordinal=NULL,payload_released_at_ms=?`,
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
		return fmt.Errorf("cleanupjobs/release game evidence: %w", err)
	}
	return nil
}
