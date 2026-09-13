package payloadrelease

import (
	"database/sql"
	"strings"
	"testing"
)

func seedEffectGamePayload(t *testing.T, database *sql.DB) {
	t.Helper()
	_, err := database.ExecContext(
		t.Context(),
		`
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES('effect-blob',?,1,?,?,?,'image/png',10);
INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,version,expires_at_ms,created_at_ms,updated_at_ms)
VALUES('effect-upload','COMPLETE','FILES',1,1,?,1,10000,10,10);
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,final_blob_id,state,created_at_ms,updated_at_ms)
VALUES('effect-file','effect-upload','asset.png',1,1,'effect-blob','COMPLETE',10,10);
INSERT INTO game_assets(id,game_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms)
VALUES('effect-asset','schedule-game','effect-blob','COVER',0,1,1,'image/png',10);
INSERT INTO upload_consumptions(id,upload_session_id,upload_file_id,consumer_type,consumer_id,version,created_at_ms)
VALUES('effect-consumption','effect-upload','effect-file','GAME_ASSET','effect-asset',1,10)`,
		strings.Repeat("e", 64),
		strings.Repeat("e", 32),
		strings.Repeat("e", 40),
		strings.Repeat("e", 8),
		strings.Repeat("f", 64),
	)
	if err != nil {
		t.Fatal(err)
	}
}

func assertEffectGameGraphRetained(t *testing.T, fixture releaseWorkerFixture) {
	t.Helper()
	assertEffectRetained(t, fixture, ScopeGame)
	assertEffectRetained(t, fixture, ScopeUploadConsumption)
	var assets int
	var state, blob string
	err := fixture.database.QueryRowContext(t.Context(), `SELECT
 (SELECT count(*) FROM game_assets WHERE game_id='schedule-game' AND blob_id='effect-blob'),
 state,COALESCE(final_blob_id,'') FROM upload_files WHERE id='effect-file'`).Scan(&assets, &state, &blob)
	if err != nil || assets != 1 || state != "COMPLETE" || blob != "effect-blob" {
		t.Fatalf(
			"partial graph assets=%d file=%s blob=%s error=%v",
			assets,
			state,
			blob,
			err,
		)
	}
	assertNoGCSchedule(t, fixture.database)
}

func assertEffectGameGraphReleased(t *testing.T, fixture releaseWorkerFixture) {
	t.Helper()
	var payload, file string
	var assets, consumptions, candidates int
	err := fixture.database.QueryRowContext(t.Context(), `SELECT
 (SELECT payload_state FROM games WHERE id='schedule-game'),
 (SELECT state FROM upload_files WHERE id='effect-file'),
 (SELECT count(*) FROM game_assets WHERE game_id='schedule-game'),
 (SELECT count(*) FROM upload_consumptions WHERE id='effect-consumption' AND released_at_ms IS NOT NULL),
 (SELECT count(*) FROM blob_gc_candidates WHERE blob_id='effect-blob')`).Scan(
		&payload,
		&file,
		&assets,
		&consumptions,
		&candidates,
	)
	if err != nil || payload != "RELEASED" || file != "PURGED" || assets != 0 || consumptions != 1 || candidates != 1 {
		t.Fatalf(
			"graph=%s %s assets=%d consumption=%d GC=%d error=%v",
			payload,
			file,
			assets,
			consumptions,
			candidates,
			err,
		)
	}
}
