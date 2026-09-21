package payloadrelease

import (
	"context"

	"retrom/internal/dbexec"
)

func GameBlobIDs(ctx context.Context, transaction dbexec.Executor, gameID string) ([]string, error) {
	return collectIDs(ctx, transaction, `
SELECT blob_id FROM game_assets WHERE game_id=?
UNION ALL SELECT file.blob_id FROM game_files file
 WHERE file.game_id=?
UNION ALL SELECT file.source_archive_blob_id FROM game_files file
 WHERE file.game_id=?
UNION ALL SELECT file.blob_id FROM variant_files file
 JOIN game_variants variant ON variant.id=file.game_variant_id WHERE variant.game_id=?
UNION ALL SELECT binding.restore_payload_blob_id FROM launch_game_save_bindings binding
 JOIN launch_sessions launch ON launch.id=binding.launch_session_id WHERE launch.game_id=?
UNION ALL SELECT payload_blob_id FROM save_states WHERE game_id=?
UNION ALL SELECT screenshot_blob_id FROM save_states WHERE game_id=?
UNION ALL SELECT file.blob_id FROM launch_content_files file
 JOIN launch_sessions launch ON launch.id=file.launch_session_id WHERE launch.game_id=?
UNION ALL SELECT file.blob_id FROM launch_external_files file
 JOIN launch_sessions launch ON launch.id=file.launch_session_id WHERE launch.game_id=?
UNION ALL SELECT evidence.blob_id FROM content_hash_evidence evidence
 JOIN metadata_scrape_runs run ON run.id=evidence.scrape_run_id WHERE run.game_id=?
UNION ALL SELECT evidence.archive_blob_id FROM content_hash_evidence evidence
 JOIN metadata_scrape_runs run ON run.id=evidence.scrape_run_id WHERE run.game_id=?
UNION ALL SELECT asset.blob_id FROM scrape_candidate_assets asset
 JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
 JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id WHERE run.game_id=?
`, gameID, gameID, gameID, gameID, gameID, gameID, gameID, gameID, gameID, gameID, gameID, gameID)
}
