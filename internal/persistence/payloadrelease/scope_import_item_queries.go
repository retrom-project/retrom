package payloadrelease

import (
	"context"

	"retrom/internal/dbexec"
)

func ImportItemBlobIDs(ctx context.Context, transaction dbexec.Executor, itemID string) ([]string, error) {
	return collectIDs(ctx, transaction, `
SELECT blob_id FROM import_item_source_files WHERE import_item_id=?
UNION ALL SELECT source_archive_blob_id FROM import_item_source_files WHERE import_item_id=?
UNION ALL SELECT file.blob_id FROM import_item_source_snapshot_files file
 JOIN import_item_source_snapshots snapshot ON snapshot.id=file.source_snapshot_id
 WHERE snapshot.import_item_id=?
UNION ALL SELECT file.source_archive_blob_id FROM import_item_source_snapshot_files file
 JOIN import_item_source_snapshots snapshot ON snapshot.id=file.source_snapshot_id
 WHERE snapshot.import_item_id=?
UNION ALL SELECT entry.blob_id FROM import_item_multidisc_entries entry
 JOIN import_item_source_snapshots snapshot ON snapshot.id=entry.source_snapshot_id
 WHERE snapshot.import_item_id=?
UNION ALL SELECT file.blob_id FROM import_item_validation_files file
 JOIN import_item_core_validations validation ON validation.id=file.import_item_core_validation_id
 WHERE validation.import_item_id=?
UNION ALL SELECT blob_id FROM review_uploaded_assets WHERE import_item_id=?
UNION ALL SELECT accepted_blob_id FROM review_arcade_parent_attachments WHERE import_item_id=?
UNION ALL SELECT content_blob_id FROM review_preview_sessions WHERE import_item_id=?
UNION ALL SELECT checkpoint_payload_blob_id FROM review_preview_sessions WHERE import_item_id=?
UNION ALL SELECT restore_payload_blob_id FROM review_preview_sessions WHERE import_item_id=?
UNION ALL SELECT file.blob_id FROM review_preview_files file
 JOIN review_preview_sessions preview ON preview.id=file.preview_session_id WHERE preview.import_item_id=?
UNION ALL SELECT blob_id FROM review_runtime_screenshots WHERE import_item_id=?
UNION ALL SELECT evidence.blob_id FROM content_hash_evidence evidence
 JOIN metadata_scrape_runs run ON run.id=evidence.scrape_run_id WHERE run.import_item_id=?
UNION ALL SELECT evidence.archive_blob_id FROM content_hash_evidence evidence
 JOIN metadata_scrape_runs run ON run.id=evidence.scrape_run_id WHERE run.import_item_id=?
UNION ALL SELECT asset.blob_id FROM scrape_candidate_assets asset
 JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
 JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id WHERE run.import_item_id=?
UNION ALL SELECT file.blob_id FROM pegasus_import_item_files file
 JOIN pegasus_import_items item ON item.id=file.item_id WHERE item.library_import_item_id=?
UNION ALL SELECT file.source_archive_blob_id FROM pegasus_import_item_files file
 JOIN pegasus_import_items item ON item.id=file.item_id WHERE item.library_import_item_id=?
UNION ALL SELECT asset.blob_id FROM pegasus_import_item_assets asset
 JOIN pegasus_import_items item ON item.id=asset.item_id WHERE item.library_import_item_id=?
UNION ALL SELECT file.blob_id FROM emulationstation_import_item_files file
 JOIN emulationstation_import_items item ON item.id=file.item_id WHERE item.library_import_item_id=?
UNION ALL SELECT file.source_archive_blob_id FROM emulationstation_import_item_files file
 JOIN emulationstation_import_items item ON item.id=file.item_id WHERE item.library_import_item_id=?
UNION ALL SELECT asset.blob_id FROM emulationstation_import_item_assets asset
 JOIN emulationstation_import_items item ON item.id=asset.item_id WHERE item.library_import_item_id=?
`, itemID, itemID, itemID, itemID, itemID, itemID, itemID, itemID, itemID, itemID, itemID,
		itemID, itemID, itemID, itemID, itemID, itemID, itemID, itemID, itemID, itemID, itemID)
}
