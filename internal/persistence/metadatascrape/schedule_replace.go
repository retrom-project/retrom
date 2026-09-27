package metadatascrape

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"

	application "retrom/internal/service/metadatascrape"
)

// A subject owns one current scrape. Replacing it fences pending workers and
// removes its candidates, evidence and media; published game assets are independent.
func (writes scheduleWrites) replaceCurrent(ctx context.Context, plan application.SchedulePlan) error {
	predicate := "import_item_id=?"
	if plan.Subject.Kind == "GAME" {
		predicate = "game_id=?"
	}
	current := "SELECT id FROM metadata_scrape_runs WHERE " + predicate
	_, err := writes.transaction.ExecContext(ctx, `UPDATE jobs SET state='CANCELLED',
 cancel_requested_at_ms=COALESCE(cancel_requested_at_ms,?),cancel_reason='SCRAPE_REPLACED',
 finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,version=version+1,updated_at_ms=?
 WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED') AND (
 id IN (SELECT job_id FROM metadata_scrape_runs WHERE `+predicate+`) OR
 id IN (SELECT a.media_fetch_job_id FROM scrape_candidate_assets a
 JOIN scrape_candidates c ON c.id=a.scrape_candidate_id WHERE c.scrape_run_id IN (`+current+`)))`,
		plan.Now, plan.Now, plan.Now, plan.Subject.ID, plan.Subject.ID)
	if err != nil {
		return fmt.Errorf("cancel replaced scrape jobs: %w", err)
	}
	if _, err := writes.transaction.ExecContext(ctx, `UPDATE stored_files SET retired_at_ms=COALESCE(retired_at_ms,?)
WHERE owner_kind='SCRAPE_RUN' AND owner_id IN (`+current+`)`, plan.Now, plan.Subject.ID); err != nil {
		return fmt.Errorf("retire replaced scrape files: %w", err)
	}
	if _, err := recordstore.DeleteScrapeCandidateAssets(
		ctx,
		writes.transaction,
		recordstore.Scope{
			Where: "scrape_candidate_id IN (SELECT id FROM scrape_candidates WHERE scrape_run_id IN (" + current + "))",
			Args:  []any{plan.Subject.ID},
		},
	); err != nil {
		return fmt.Errorf("release replaced scrape media: %w", err)
	}
	if _, err := recordstore.DeleteRows(ctx, writes.transaction, "content_hash_evidence", recordstore.Scope{
		Where: "scrape_run_id IN (" + current + ")", Args: []any{plan.Subject.ID},
	}); err != nil {
		return fmt.Errorf("release replaced scrape evidence: %w", err)
	}
	_, err = writes.transaction.ExecContext(ctx, "DELETE FROM metadata_scrape_runs WHERE "+predicate, plan.Subject.ID)
	if err != nil {
		return fmt.Errorf("replace current scrape: %w", err)
	}
	return nil
}
