package references

import (
	"context"

	dbapi "retrom/internal/database"
)

func GameFileRecords(ctx context.Context, transaction dbapi.Executor, gameID string) ([]string, error) {
	return wrapPair(dbapi.QueryStrings(ctx, transaction, `
SELECT file_record FROM game_assets WHERE game_id=?
UNION ALL SELECT file.file_record FROM game_files file
 WHERE file.game_id=?
UNION ALL SELECT file.source_archive_file_record FROM game_files file
 WHERE file.game_id=?
UNION ALL SELECT file.file_record FROM variant_files file
 JOIN game_variants variant ON variant.id=file.game_variant_id WHERE variant.game_id=?
 AND file.role<>'BIOS_BUNDLE'
UNION ALL SELECT payload_file_record FROM save_states WHERE game_id=?
UNION ALL SELECT screenshot_file_record FROM save_states WHERE game_id=?
UNION ALL SELECT evidence.file_record FROM content_hash_evidence evidence
 JOIN metadata_scrape_runs run ON run.id=evidence.scrape_run_id WHERE run.game_id=?
UNION ALL SELECT evidence.archive_file_record FROM content_hash_evidence evidence
 JOIN metadata_scrape_runs run ON run.id=evidence.scrape_run_id WHERE run.game_id=?
UNION ALL SELECT asset.file_record FROM scrape_candidate_assets asset
 JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
 JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id WHERE run.game_id=?
`, gameID, gameID, gameID, gameID, gameID, gameID, gameID, gameID, gameID))
}
