package itemrelease

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	"retrom/internal/persistence/filedeletion"
	"retrom/internal/persistence/recordstore"
)

func (records Records) ClearWorkingRecords(ctx context.Context, itemID string, now int64) error {
	remaining, err := records.ItemRemaining(ctx, itemID)
	if err != nil || remaining != 0 {
		return err
	}
	if _, err := recordstore.UpdateReviewItems(ctx, records.Executor, recordstore.Update{
		Set: `selected_candidate_id=NULL,effective_source_snapshot_id=NULL,review_profile_json=NULL,
 content_analysis_json='{}',metadata_json=NULL,default_dos_entry=NULL`,
		Scope: recordstore.Scope{Where: `id=? AND payload_state='RELEASING'
 AND state IN ('PUBLISHED','DISCARDED','CANCELLED','FAILED_FINAL')`, Args: []any{itemID}},
	}); err != nil {
		return fmt.Errorf("clear completed review working fields: %w", err)
	}
	if err := records.removeScrapeDirectories(ctx, itemID, now); err != nil {
		return err
	}
	for _, deletion := range completedReviewRecords {
		if _, err := recordstore.DeleteRows(ctx, records.Executor, deletion.table,
			recordstore.Scope{Where: deletion.where, Args: []any{itemID}}); err != nil {
			return fmt.Errorf("remove completed %s: %w", deletion.table, err)
		}
	}
	return nil
}

var completedReviewRecords = []struct{ table, where string }{
	{"review_draft_tags", "review_draft_id=?"},
	{"import_item_dos_entries", "import_item_id=?"},
	{"review_arcade_parent_attachments", "import_item_id=?"},
	{"review_multidisc_attachments", "import_item_id=?"},
	{"import_item_multidisc_entries", `source_snapshot_id IN (
SELECT id FROM import_item_source_snapshots WHERE import_item_id=?)`},
	{"import_item_source_snapshots", "import_item_id=?"},
	{"content_hash_evidence", "scrape_run_id IN (SELECT id FROM metadata_scrape_runs WHERE import_item_id=?)"},
	{"metadata_scrape_runs", "import_item_id=?"},
}

func (records Records) removeScrapeDirectories(ctx context.Context, itemID string, now int64) error {
	ids, err := dbapi.QueryStrings(ctx, records.Executor,
		`SELECT id FROM metadata_scrape_runs WHERE import_item_id=?`, itemID)
	if err != nil {
		return fmt.Errorf("read retired scrape directories: %w", err)
	}
	for _, id := range ids {
		if !filestore.RemovablePath("scrapes/" + id) {
			continue
		}
		if err := filedeletion.QueuePath(ctx, records.Executor, "scrapes/"+id, now); err != nil {
			return fmt.Errorf("queue retired scrape directory: %w", err)
		}
	}
	return nil
}
