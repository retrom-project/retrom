package gamecontent

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
)

func gameReferenceCount(ctx context.Context, transaction dbapi.Executor, gameID, blobID string) (int64, error) {
	var count int64
	err := dbapi.QueryRowContext(ctx, transaction, `
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
)
`, gameID, blobID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("gamecontent/delete impact scoped refs: %w", err)
	}
	return count, nil
}
