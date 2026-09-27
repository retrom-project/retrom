package gamerelease

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/releaseops"
)

func (records Records) ClearEvidence(ctx context.Context, gameID string, now int64) error {
	if _, err := records.Executor.ExecContext(ctx, `UPDATE stored_files SET retired_at_ms=COALESCE(retired_at_ms,?)
 WHERE (owner_kind='SAVE_STATE' AND owner_id IN(SELECT id FROM save_states WHERE game_id=?))
 OR (owner_kind='SCRAPE_RUN' AND owner_id IN(SELECT id FROM metadata_scrape_runs WHERE
game_id=?))`, now, gameID, gameID); err != nil {
		return fmt.Errorf("retire game auxiliary files: %w", err)
	}

	if err := (releaseops.Records{Executor: records.Executor}).CheckedUpdate(
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
		return fmt.Errorf("cleanupjobs/release game evidence: %w", err)
	}
	return nil
}
