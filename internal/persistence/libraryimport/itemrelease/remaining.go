package itemrelease

import (
	"context"

	"retrom/internal/persistence/releaseops"
)

func (records Records) ItemRemaining(ctx context.Context, id string) (int64, error) {
	return wrapPair(
		(releaseops.Records{Executor: records.Executor}).ReadCount(
			ctx,
			`
SELECT
 (SELECT count(*) FROM review_draft_screenshot_assets WHERE review_draft_id=?)+
  (SELECT count(*) FROM isolated_runtime_capabilities capability JOIN review_preview_sessions preview
   ON preview.id=capability.preview_id WHERE preview.import_item_id=?)+
  (SELECT count(*) FROM isolated_runtime_bootstrap_tickets ticket JOIN review_preview_sessions preview
   ON preview.id=ticket.preview_id WHERE preview.import_item_id=?)+
  (SELECT count(*) FROM review_preview_files file JOIN review_preview_sessions preview
   ON preview.id=file.preview_session_id WHERE preview.import_item_id=?)+
  (SELECT count(*) FROM review_runtime_screenshots WHERE import_item_id=?)+
  (SELECT count(*) FROM review_arcade_parent_attachments WHERE import_item_id=? AND accepted_blob_id IS NOT
NULL)+
  (SELECT count(*) FROM import_item_multidisc_entries entry JOIN import_item_source_snapshots snapshot
   ON snapshot.id=entry.source_snapshot_id WHERE snapshot.import_item_id=? AND entry.blob_id IS NOT NULL)+
  (SELECT count(*) FROM import_item_source_files WHERE import_item_id=?)+
  (SELECT count(*) FROM import_item_source_snapshot_files file
   JOIN import_item_source_snapshots snapshot ON snapshot.id=file.source_snapshot_id
   WHERE snapshot.import_item_id=?)+
  (SELECT count(*) FROM import_item_validation_files file
   JOIN import_item_core_validations validation ON validation.id=file.import_item_core_validation_id
   WHERE validation.import_item_id=?)+
  (SELECT count(*) FROM review_uploaded_assets WHERE import_item_id=?)+
  (SELECT count(*) FROM review_preview_sessions WHERE import_item_id=?)+
  (SELECT count(*) FROM content_hash_evidence evidence
   JOIN metadata_scrape_runs run ON run.id=evidence.scrape_run_id
   WHERE run.import_item_id=? AND evidence.payload_released_at_ms IS NULL)+
  (SELECT count(*) FROM scrape_candidate_assets asset
   JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
   JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id WHERE run.import_item_id=?)+
  (SELECT count(*) FROM import_item_assets WHERE import_item_id=?)
`,
			id,
			id,
			id,
			id,
			id,
			id,
			id,
			id,
			id,
			id,
			id,
			id,
			id,
			id,
			id,
		),
	)
}

func (records Records) JobRemaining(ctx context.Context, id string) (int64, error) {
	return wrapPair((releaseops.Records{Executor: records.Executor}).ReadCount(ctx, `
SELECT (SELECT count(*) FROM import_items WHERE import_job_id=? AND payload_state<>'RELEASED')+
       (SELECT count(*) FROM upload_consumptions
        WHERE consumer_type='IMPORT_JOB' AND consumer_id=? AND released_at_ms IS NULL)
`, id, id))
}
