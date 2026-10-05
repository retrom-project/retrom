package itemrelease

import (
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/releaseops"
)

func DeleteStatements() []releaseops.DeletionBatch {
	return []releaseops.DeletionBatch{
		{Table: "import_item_assets", Remove: recordstore.DeleteImportItemAssets, Where: `import_item_id=?`},

		{
			Table:  "isolated_runtime_capabilities",
			Remove: recordstore.DeleteIsolatedRuntimeCapabilities,
			Where: `ctid IN (
 SELECT capability.ctid FROM isolated_runtime_capabilities capability
 JOIN runtime_preview_sessions preview ON preview.id=capability.preview_id
 WHERE preview.id IN (
 SELECT preview_session_id FROM review_preview_bindings WHERE import_item_id=?)
 AND preview.state IN ('EXPIRED','REVOKED')
 ORDER BY capability.ctid LIMIT 200
)`,
		},

		{
			Table:  "isolated_runtime_bootstrap_tickets",
			Remove: recordstore.DeleteIsolatedRuntimeBootstrapTickets,
			Where: `ctid IN (
 SELECT ticket.ctid FROM isolated_runtime_bootstrap_tickets ticket
 JOIN runtime_preview_sessions preview ON preview.id=ticket.preview_id
 WHERE preview.id IN (
 SELECT preview_session_id FROM review_preview_bindings WHERE import_item_id=?)
 AND preview.state IN ('EXPIRED','REVOKED')
 ORDER BY ticket.ctid LIMIT 200
)`,
		},

		{Table: "runtime_preview_files", Remove: recordstore.DeleteRuntimePreviewFiles, Where: `ctid IN (
 SELECT file.ctid FROM runtime_preview_files file
 JOIN runtime_preview_sessions preview ON preview.id=file.preview_session_id
 WHERE preview.id IN (
 SELECT preview_session_id FROM review_preview_bindings WHERE import_item_id=?) ORDER BY file.ctid LIMIT 200
)`},

		{
			Table:  "review_runtime_screenshots",
			Remove: recordstore.DeleteReviewRuntimeScreenshots,
			Where: `ctid IN (
 SELECT ctid FROM review_runtime_screenshots
 WHERE import_item_id=? ORDER BY ctid LIMIT 200
)`,
		},

		{
			Table:  "runtime_preview_sessions",
			Remove: recordstore.DeleteRuntimePreviewSessions,
			Where: `ctid IN (
 SELECT ctid FROM runtime_preview_sessions WHERE id IN (
 SELECT preview_session_id FROM review_preview_bindings WHERE import_item_id=?)
AND state IN ('EXPIRED','REVOKED')
AND NOT EXISTS(SELECT 1 FROM isolated_runtime_capabilities WHERE preview_id=runtime_preview_sessions.id)
AND NOT EXISTS(SELECT 1 FROM isolated_runtime_bootstrap_tickets WHERE preview_id=runtime_preview_sessions.id)
AND NOT EXISTS(SELECT 1 FROM runtime_preview_files WHERE preview_session_id=runtime_preview_sessions.id)
ORDER BY ctid LIMIT 200
)`,
		},

		{Table: "review_uploaded_assets", Remove: recordstore.DeleteReviewUploadedAssets, Where: `ctid IN (
 SELECT ctid FROM review_uploaded_assets WHERE import_item_id=? ORDER BY ctid LIMIT 200
)`},

		{Table: "scrape_candidate_assets", Remove: recordstore.DeleteScrapeCandidateAssets, Where: `ctid IN (
 SELECT asset.ctid FROM scrape_candidate_assets asset
 JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
 JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id
 WHERE run.import_item_id=? ORDER BY asset.ctid LIMIT 200
)`},

		{
			Table:  "import_item_runtime_files",
			Remove: recordstore.DeleteImportItemRuntimeFiles,
			Where: `ctid IN (
 SELECT file.ctid FROM import_item_runtime_files file
 WHERE file.import_item_id=? ORDER BY file.ctid LIMIT 200
)`,
		},

		{
			Table:  "import_item_source_snapshot_files",
			Remove: recordstore.DeleteImportItemSourceSnapshotFiles,
			Where: `ctid IN (
 SELECT file.ctid FROM import_item_source_snapshot_files file
 JOIN import_item_source_snapshots snapshot ON snapshot.id=file.source_snapshot_id
 WHERE snapshot.import_item_id=? ORDER BY file.ctid LIMIT 200
)`,
		},

		{Table: "import_item_source_files", Remove: recordstore.DeleteImportItemSourceFiles, Where: `ctid IN (
 SELECT ctid FROM import_item_source_files WHERE import_item_id=? ORDER BY ctid LIMIT 200
)`},
	}
}
