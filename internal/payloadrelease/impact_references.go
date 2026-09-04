package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"
)

func gameReferenceCount(ctx context.Context, transaction *sql.Tx, gameID, blobID string) (int64, error) {
	var count int64
	err := transaction.QueryRowContext(ctx, `
WITH game_import_items(id) AS (
 SELECT metadata_source_ref_id FROM games
 WHERE id=?1 AND metadata_source_kind='IMPORT_REVIEW'
 UNION SELECT content_source_ref_id FROM games
 WHERE id=?1 AND content_source_kind='IMPORT_REVIEW'
), game_pegasus_items(id) AS (
 SELECT metadata_source_ref_id FROM games
 WHERE id=?1 AND metadata_source_kind='SERVER_PEGASUS_IMPORT'
 UNION SELECT content_source_ref_id FROM games
 WHERE id=?1 AND content_source_kind='SERVER_PEGASUS_IMPORT'
), game_emulationstation_items(id) AS (
 SELECT metadata_source_ref_id FROM games
 WHERE id=?1 AND metadata_source_kind='SERVER_EMULATIONSTATION_IMPORT'
 UNION SELECT content_source_ref_id FROM games
 WHERE id=?1 AND content_source_kind='SERVER_EMULATIONSTATION_IMPORT'
)
SELECT count(*) FROM (
 SELECT asset.id FROM game_assets asset WHERE asset.game_id=?1 AND asset.blob_id=?2
 UNION ALL SELECT file.rowid FROM game_files file
 WHERE file.game_id=?1 AND file.blob_id=?2
 UNION ALL SELECT file.rowid FROM game_files file
 WHERE file.game_id=?1 AND file.source_archive_blob_id=?2
 UNION ALL SELECT file.rowid FROM variant_files file
 JOIN game_variants variant ON variant.id=file.game_variant_id
 WHERE variant.game_id=?1 AND file.blob_id=?2
 UNION ALL SELECT save.id FROM save_states save
 WHERE save.game_id=?1 AND save.payload_blob_id=?2
 UNION ALL SELECT save.id FROM save_states save
 WHERE save.game_id=?1 AND save.screenshot_blob_id=?2
 UNION ALL SELECT file.launch_session_id FROM launch_content_files file
 JOIN launch_sessions launch ON launch.id=file.launch_session_id
 WHERE launch.game_id=?1 AND file.blob_id=?2
 UNION ALL SELECT file.rowid FROM launch_external_files file
 JOIN launch_sessions launch ON launch.id=file.launch_session_id
 WHERE launch.game_id=?1 AND file.blob_id=?2
 UNION ALL SELECT evidence.id FROM content_hash_evidence evidence
 JOIN metadata_scrape_runs run ON run.id=evidence.scrape_run_id
 WHERE run.game_id=?1 AND evidence.blob_id=?2
 UNION ALL SELECT evidence.id FROM content_hash_evidence evidence
 JOIN metadata_scrape_runs run ON run.id=evidence.scrape_run_id
 WHERE run.game_id=?1 AND evidence.archive_blob_id=?2
 UNION ALL SELECT asset.id FROM scrape_candidate_assets asset
 JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
 JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id
 WHERE run.game_id=?1 AND asset.blob_id=?2
 UNION ALL SELECT file.rowid FROM import_item_source_files file
 WHERE file.import_item_id IN (SELECT id FROM game_import_items) AND file.blob_id=?2
 UNION ALL SELECT file.rowid FROM import_item_source_files file
 WHERE file.import_item_id IN (SELECT id FROM game_import_items) AND file.source_archive_blob_id=?2
 UNION ALL SELECT file.rowid FROM import_item_source_snapshot_files file
 JOIN import_item_source_snapshots snapshot ON snapshot.id=file.source_snapshot_id
 WHERE snapshot.import_item_id IN (SELECT id FROM game_import_items) AND file.blob_id=?2
 UNION ALL SELECT file.rowid FROM import_item_source_snapshot_files file
 JOIN import_item_source_snapshots snapshot ON snapshot.id=file.source_snapshot_id
 WHERE snapshot.import_item_id IN (SELECT id FROM game_import_items) AND file.source_archive_blob_id=?2
 UNION ALL SELECT entry.rowid FROM import_item_multidisc_entries entry
 JOIN import_item_source_snapshots snapshot ON snapshot.id=entry.source_snapshot_id
 WHERE snapshot.import_item_id IN (SELECT id FROM game_import_items) AND entry.blob_id=?2
 UNION ALL SELECT file.rowid FROM import_item_validation_files file
 JOIN import_item_core_validations validation ON validation.id=file.import_item_core_validation_id
 WHERE validation.import_item_id IN (SELECT id FROM game_import_items) AND file.blob_id=?2
 UNION ALL SELECT asset.id FROM review_uploaded_assets asset
 WHERE asset.import_item_id IN (SELECT id FROM game_import_items) AND asset.blob_id=?2
 UNION ALL SELECT attachment.id FROM review_arcade_parent_attachments attachment
 WHERE attachment.import_item_id IN (SELECT id FROM game_import_items) AND attachment.accepted_blob_id=?2
 UNION ALL SELECT preview.id FROM review_preview_sessions preview
 WHERE preview.import_item_id IN (SELECT id FROM game_import_items) AND preview.content_blob_id=?2
 UNION ALL SELECT file.rowid FROM review_preview_files file
 JOIN review_preview_sessions preview ON preview.id=file.preview_session_id
 WHERE preview.import_item_id IN (SELECT id FROM game_import_items) AND file.blob_id=?2
 UNION ALL SELECT screenshot.id FROM review_runtime_screenshots screenshot
 WHERE screenshot.import_item_id IN (SELECT id FROM game_import_items) AND screenshot.blob_id=?2
 UNION ALL SELECT evidence.id FROM content_hash_evidence evidence
 JOIN metadata_scrape_runs run ON run.id=evidence.scrape_run_id
 WHERE run.import_item_id IN (SELECT id FROM game_import_items) AND evidence.blob_id=?2
 UNION ALL SELECT evidence.id FROM content_hash_evidence evidence
 JOIN metadata_scrape_runs run ON run.id=evidence.scrape_run_id
 WHERE run.import_item_id IN (SELECT id FROM game_import_items) AND evidence.archive_blob_id=?2
 UNION ALL SELECT asset.id FROM scrape_candidate_assets asset
 JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
 JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id
 WHERE run.import_item_id IN (SELECT id FROM game_import_items) AND asset.blob_id=?2
 UNION ALL SELECT file.rowid FROM pegasus_import_item_files file
 JOIN pegasus_import_items item ON item.id=file.item_id
 WHERE item.library_import_item_id IN (SELECT id FROM game_import_items) AND file.blob_id=?2
 UNION ALL SELECT file.rowid FROM pegasus_import_item_files file
 JOIN pegasus_import_items item ON item.id=file.item_id
 WHERE item.library_import_item_id IN (SELECT id FROM game_import_items) AND file.source_archive_blob_id=?2
 UNION ALL SELECT asset.rowid FROM pegasus_import_item_assets asset
 JOIN pegasus_import_items item ON item.id=asset.item_id
 WHERE item.library_import_item_id IN (SELECT id FROM game_import_items) AND asset.blob_id=?2
 UNION ALL SELECT file.rowid FROM pegasus_import_item_files file
 WHERE file.item_id IN (SELECT id FROM game_pegasus_items) AND file.blob_id=?2
 UNION ALL SELECT file.rowid FROM pegasus_import_item_files file
 WHERE file.item_id IN (SELECT id FROM game_pegasus_items) AND file.source_archive_blob_id=?2
 UNION ALL SELECT asset.rowid FROM pegasus_import_item_assets asset
 WHERE asset.item_id IN (SELECT id FROM game_pegasus_items) AND asset.blob_id=?2
 UNION ALL SELECT file.rowid FROM emulationstation_import_item_files file
 JOIN emulationstation_import_items item ON item.id=file.item_id
 WHERE item.library_import_item_id IN (SELECT id FROM game_import_items) AND file.blob_id=?2
 UNION ALL SELECT file.rowid FROM emulationstation_import_item_files file
 JOIN emulationstation_import_items item ON item.id=file.item_id
 WHERE item.library_import_item_id IN (SELECT id FROM game_import_items)
   AND file.source_archive_blob_id=?2
 UNION ALL SELECT asset.rowid FROM emulationstation_import_item_assets asset
 JOIN emulationstation_import_items item ON item.id=asset.item_id
 WHERE item.library_import_item_id IN (SELECT id FROM game_import_items) AND asset.blob_id=?2
 UNION ALL SELECT file.rowid FROM emulationstation_import_item_files file
 WHERE file.item_id IN (SELECT id FROM game_emulationstation_items) AND file.blob_id=?2
 UNION ALL SELECT file.rowid FROM emulationstation_import_item_files file
 WHERE file.item_id IN (SELECT id FROM game_emulationstation_items)
   AND file.source_archive_blob_id=?2
 UNION ALL SELECT asset.rowid FROM emulationstation_import_item_assets asset
 WHERE asset.item_id IN (SELECT id FROM game_emulationstation_items) AND asset.blob_id=?2
)
`, gameID, blobID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("payloadrelease/impact scoped refs: %w", err)
	}
	return count, nil
}
