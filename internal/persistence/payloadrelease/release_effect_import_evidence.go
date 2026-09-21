package payloadrelease

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
)

func (records effectRecords) clearImportEvidence(ctx context.Context, itemID string, now int64) error {
	if err := records.checkedUpdate(
		ctx,
		"review_arcade_parent_attachments",
		recordstore.UpdateReviewArcadeParentAttachments,
		recordstore.Update{
			Set: `accepted_blob_id=NULL,payload_released_at_ms=?,version=version+1,updated_at_ms=?`, Scope: recordstore.Scope{
				Where: `import_item_id=? AND accepted_blob_id IS NOT NULL`,
				Args:  []any{itemID},
			},
			Values: []any{now, now},
		},
	); err != nil {
		return fmt.Errorf("payloadrelease/release parent evidence: %w", err)
	}
	if err := records.checkedUpdate(
		ctx,
		"import_item_multidisc_entries",
		recordstore.UpdateImportItemMultidiscEntries,
		recordstore.Update{
			Set: `state='PAYLOAD_RELEASED',upload_file_id=NULL,blob_id=NULL,payload_released_at_ms=?`,
			Scope: recordstore.Scope{
				Where: `
blob_id IS NOT NULL AND source_snapshot_id IN (
  SELECT id FROM import_item_source_snapshots WHERE import_item_id=?
)
`,
				Args: []any{itemID},
			},
			Values: []any{now},
		},
	); err != nil {
		return fmt.Errorf("payloadrelease/release multidisc evidence: %w", err)
	}
	if err := records.checkedUpdate(
		ctx,
		"content_hash_evidence",
		recordstore.UpdateContentHashEvidence,
		recordstore.Update{
			Set: `blob_id=NULL,archive_blob_id=NULL,archive_entry_ordinal=NULL,payload_released_at_ms=?`,
			Scope: recordstore.Scope{
				Where: `
payload_released_at_ms IS NULL AND scrape_run_id IN (
  SELECT id FROM metadata_scrape_runs WHERE import_item_id=?
)
`,
				Args: []any{itemID},
			},
			Values: []any{now},
		},
	); err != nil {
		return fmt.Errorf("payloadrelease/release hash evidence: %w", err)
	}

	return nil
}
