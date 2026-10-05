package gamerelease

import (
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/releaseops"
)

func DeleteStatements() []releaseops.DeletionBatch {
	return []releaseops.DeletionBatch{
		{Table: "save_states", Remove: recordstore.DeleteSaveStates, Where: `
ctid IN (SELECT ctid FROM save_states WHERE game_id=? AND NOT EXISTS(
SELECT 1 FROM launch_sessions WHERE save_state_id=save_states.id) ORDER BY ctid LIMIT 200)`},
		{Table: "launch_external_files", Remove: recordstore.DeleteLaunchExternalFiles, Where: `ctid IN (
 SELECT file.ctid FROM launch_external_files file
 JOIN launch_sessions launch ON launch.id=file.launch_session_id
 WHERE launch.game_id=? ORDER BY file.ctid LIMIT 200
)`},
		{Table: "launch_content_files", Remove: recordstore.DeleteLaunchContentFiles, Where: `ctid IN (
 SELECT file.ctid FROM launch_content_files file
 JOIN launch_sessions launch ON launch.id=file.launch_session_id
 WHERE launch.game_id=? ORDER BY file.ctid LIMIT 200
)`},
		{Table: "scrape_candidate_assets", Remove: recordstore.DeleteScrapeCandidateAssets, Where: `ctid IN (
 SELECT asset.ctid FROM scrape_candidate_assets asset
 JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
 JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id
 WHERE run.game_id=? ORDER BY asset.ctid LIMIT 200
)`},
		{Table: "game_assets", Remove: recordstore.DeleteGameAssets, Where: `
ctid IN (SELECT ctid FROM game_assets WHERE game_id=? ORDER BY ctid LIMIT 200)`},
		{Table: "game_files", Remove: recordstore.DeleteGameFiles, Where: `ctid IN (
 SELECT file.ctid FROM game_files file
 WHERE file.game_id=? ORDER BY file.ctid LIMIT 200
)`},
		{Table: "variant_files", Remove: recordstore.DeleteVariantFiles, Where: `ctid IN (
 SELECT file.ctid FROM variant_files file
 JOIN game_variants variant ON variant.id=file.game_variant_id
 WHERE variant.game_id=? ORDER BY file.ctid LIMIT 200
)`},
	}
}
