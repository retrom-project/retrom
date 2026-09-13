package payloadrelease

import (
	"retrom/internal/repo/recordstore"
)

func gameEffectDeleteStatements() []effectDeletionBatch {
	return []effectDeletionBatch{
		{table: "save_states", remove: recordstore.DeleteSaveStates, where: `
rowid IN (SELECT rowid FROM save_states WHERE game_id=? ORDER BY rowid LIMIT 200)`},
		{table: "launch_external_files", remove: recordstore.DeleteLaunchExternalFiles, where: `rowid IN (
 SELECT file.rowid FROM launch_external_files file
 JOIN launch_sessions launch ON launch.id=file.launch_session_id
 WHERE launch.game_id=? ORDER BY file.rowid LIMIT 200
)`},
		{table: "launch_content_files", remove: recordstore.DeleteLaunchContentFiles, where: `rowid IN (
 SELECT file.rowid FROM launch_content_files file
 JOIN launch_sessions launch ON launch.id=file.launch_session_id
 WHERE launch.game_id=? ORDER BY file.rowid LIMIT 200
)`},
		{table: "scrape_candidate_assets", remove: recordstore.DeleteScrapeCandidateAssets, where: `rowid IN (
 SELECT asset.rowid FROM scrape_candidate_assets asset
 JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
 JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id
 WHERE run.game_id=? ORDER BY asset.rowid LIMIT 200
)`},
		{table: "game_assets", remove: recordstore.DeleteGameAssets, where: `
rowid IN (SELECT rowid FROM game_assets WHERE game_id=? ORDER BY rowid LIMIT 200)`},
		{table: "game_files", remove: recordstore.DeleteGameFiles, where: `rowid IN (
 SELECT file.rowid FROM game_files file
 WHERE file.game_id=? ORDER BY file.rowid LIMIT 200
)`},
		{table: "variant_files", remove: recordstore.DeleteVariantFiles, where: `rowid IN (
 SELECT file.rowid FROM variant_files file
 JOIN game_variants variant ON variant.id=file.game_variant_id
 WHERE variant.game_id=? ORDER BY file.rowid LIMIT 200
)`},
	}
}
