package payloadrelease

import (
	"retrom/internal/persistence/recordstore"
)

func importEffectDeleteStatements() []effectDeletionBatch {
	return []effectDeletionBatch{
		{table: "isolated_runtime_capabilities", remove: recordstore.DeleteIsolatedRuntimeCapabilities, where: `rowid IN (
 SELECT capability.rowid FROM isolated_runtime_capabilities capability
 JOIN review_preview_sessions preview ON preview.id=capability.preview_id
 WHERE preview.import_item_id=? AND preview.state IN ('EXPIRED','REVOKED')
 ORDER BY capability.rowid LIMIT 200
)`},
		{
			table:  "isolated_runtime_bootstrap_tickets",
			remove: recordstore.DeleteIsolatedRuntimeBootstrapTickets,
			where: `rowid IN (
 SELECT ticket.rowid FROM isolated_runtime_bootstrap_tickets ticket
 JOIN review_preview_sessions preview ON preview.id=ticket.preview_id
 WHERE preview.import_item_id=? AND preview.state IN ('EXPIRED','REVOKED')
 ORDER BY ticket.rowid LIMIT 200
)`,
		},
		{table: "review_preview_files", remove: recordstore.DeleteReviewPreviewFiles, where: `rowid IN (
 SELECT file.rowid FROM review_preview_files file
 JOIN review_preview_sessions preview ON preview.id=file.preview_session_id
 WHERE preview.import_item_id=? ORDER BY file.rowid LIMIT 200
)`},
		{table: "review_runtime_screenshots", remove: recordstore.DeleteReviewRuntimeScreenshots, where: `rowid IN (
 SELECT rowid FROM review_runtime_screenshots
 WHERE import_item_id=? ORDER BY rowid LIMIT 200
)`},
		{table: "review_preview_sessions", remove: recordstore.DeleteReviewPreviewSessions, where: `rowid IN (
 SELECT rowid FROM review_preview_sessions WHERE import_item_id=? ORDER BY rowid LIMIT 200
)`},
		{table: "review_uploaded_assets", remove: recordstore.DeleteReviewUploadedAssets, where: `rowid IN (
 SELECT rowid FROM review_uploaded_assets WHERE import_item_id=? ORDER BY rowid LIMIT 200
)`},
		{table: "scrape_candidate_assets", remove: recordstore.DeleteScrapeCandidateAssets, where: `rowid IN (
 SELECT asset.rowid FROM scrape_candidate_assets asset
 JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
 JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id
 WHERE run.import_item_id=? ORDER BY asset.rowid LIMIT 200
)`},
		{table: "import_item_validation_files", remove: recordstore.DeleteImportItemValidationFiles, where: `rowid IN (
 SELECT file.rowid FROM import_item_validation_files file
 JOIN import_item_core_validations validation ON validation.id=file.import_item_core_validation_id
 WHERE validation.import_item_id=? ORDER BY file.rowid LIMIT 200
)`},
		{
			table:  "import_item_source_snapshot_files",
			remove: recordstore.DeleteImportItemSourceSnapshotFiles,
			where: `rowid IN (
 SELECT file.rowid FROM import_item_source_snapshot_files file
 JOIN import_item_source_snapshots snapshot ON snapshot.id=file.source_snapshot_id
 WHERE snapshot.import_item_id=? ORDER BY file.rowid LIMIT 200
)`,
		},
		{table: "import_item_source_files", remove: recordstore.DeleteImportItemSourceFiles, where: `rowid IN (
 SELECT rowid FROM import_item_source_files WHERE import_item_id=? ORDER BY rowid LIMIT 200
)`},
	}
}
