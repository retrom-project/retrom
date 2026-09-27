package gamerelease

import (
	"context"
	"retrom/internal/persistence/releaseops"
)

func (records Records) Remaining(ctx context.Context, id string) (int64, error) {
	return (releaseops.Records{Executor: records.Executor}).ReadCount(ctx, `
SELECT
 (SELECT count(*) FROM game_assets WHERE game_id=?)+
 (SELECT count(*) FROM game_files file
  WHERE file.game_id=?)+
 (SELECT count(*) FROM variant_files file
  JOIN game_variants variant ON variant.id=file.game_variant_id
  WHERE variant.game_id=?)+
 (SELECT count(*) FROM save_states WHERE game_id=?)+
 (SELECT count(*) FROM launch_content_files file
  JOIN launch_sessions launch ON launch.id=file.launch_session_id WHERE launch.game_id=?)+
 (SELECT count(*) FROM launch_external_files file
  JOIN launch_sessions launch ON launch.id=file.launch_session_id WHERE launch.game_id=?)+
 (SELECT count(*) FROM content_hash_evidence evidence
  JOIN metadata_scrape_runs run ON run.id=evidence.scrape_run_id
  WHERE run.game_id=? AND evidence.payload_released_at_ms IS NULL)+
 (SELECT count(*) FROM scrape_candidate_assets asset
  JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
  JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id WHERE run.game_id=?)
`, id, id, id, id, id, id, id, id)
}
