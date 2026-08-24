package payloadrelease

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

func TestProviderPayloadTTLWaitsForRunningScrape(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.UnixMilli(1_786_000_000_000)
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), func() time.Time { return now })
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	register := func(id, value string) blobstore.Metadata {
		t.Helper()
		metadata, putErr := blobs.Put(bytes.NewBufferString(value))
		testassert.False(t, putErr != nil, putErr)
		_, insertErr := database.SQL.ExecContext(ctx, `
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES(?,?,?,?,?,?,?,?)
`, id, metadata.SHA256, metadata.Size, metadata.MD5, metadata.SHA1, metadata.CRC32,
			"application/json", now.UnixMilli())
		testassert.False(t, insertErr != nil, insertErr)
		return metadata
	}
	free := register("provider-free", `{"free":true}`)
	busy := register("provider-busy", `{"busy":true}`)
	instanceID := testsupport.MustPlatformInstanceID(t, database.SQL, "gba/mgba")
	seedProviderRunningScrape(t, database.SQL, instanceID, free, busy, now)

	service, err := New(database.SQL, blobs, func() time.Time { return now }, 7*24*time.Hour)
	testassert.False(t, err != nil, err)
	testassert.False(t, service.ReconcileGC(ctx) != nil)
	assertProviderPayloadState(t, database.SQL, "response-free", "RELEASED")
	assertProviderPayloadState(t, database.SQL, "response-busy", "RETAINED")
	var candidates int
	testassert.False(t, database.SQL.QueryRowContext(ctx, `SELECT count(*) FROM blob_gc_candidates WHERE blob_id='provider-free'`).Scan(&candidates) != nil)
	testassert.Falsef(t, candidates != 1, "candidate count = %d", candidates)

	_, err = database.SQL.ExecContext(ctx, `
UPDATE metadata_scrape_runs SET state='COMPLETED',completed_at_ms=?,updated_at_ms=?,version=version+1
WHERE id='provider-run'
`, now.UnixMilli(), now.UnixMilli())
	testassert.False(t, err != nil, err)
	testassert.False(t, service.ReconcileGC(ctx) != nil)
	assertProviderPayloadState(t, database.SQL, "response-busy", "RELEASED")
	// The hash evidence remains a protective reference, so releasing the raw
	// provider response must not schedule the shared Blob for collection.
	testassert.False(t, database.SQL.QueryRowContext(ctx, `SELECT count(*) FROM blob_gc_candidates WHERE blob_id='provider-busy'`).Scan(&candidates) != nil)
	testassert.Falsef(t, candidates != 0, "candidate count = %d", candidates)
}

func seedProviderRunningScrape(
	t *testing.T,
	database *sql.DB,
	instanceID string,
	free, busy blobstore.Metadata,
	now time.Time,
) {
	t.Helper()
	transaction, err := database.BeginTx(t.Context(), nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	statements := []struct {
		query string
		args  []any
	}{
		{`PRAGMA defer_foreign_keys=ON`, nil},
		{`INSERT INTO game_metadata_revisions(id,game_id,title,title_initial,description,developer,publisher,genre,source_kind,created_at_ms)
VALUES('provider-meta','provider-game','Provider game','P','','','','','ADMIN_EDIT',?)`, []any{now.UnixMilli()}},
		{`INSERT INTO game_content_revisions(id,game_id,content_kind,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms)
VALUES('provider-content','provider-game','SINGLE_FILE','ADMIN_REPLACE','provider-source','{}',?,?)`, []any{digest64("1"), now.UnixMilli()}},
		{`INSERT INTO games(id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,search_text,version,created_at_ms,updated_at_ms)
VALUES('provider-game',?,'PUBLISHED','provider-meta','provider-content','provider game',1,?,?)`, []any{instanceID, now.UnixMilli(), now.UnixMilli()}},
		{`INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,attempt_count,max_attempts,version,available_at_ms,execution_started_at_ms,execution_deadline_at_ms,leased_until_ms,heartbeat_at_ms,worker_id,created_at_ms,updated_at_ms)
VALUES('provider-job','GAME','provider-game','METADATA_SCRAPE',?,1,'{}',1,'RUNNING',1,4,1,?,?,?,?,?,'fixture',?,?)`, []any{digest64("2"), now.UnixMilli(), now.UnixMilli(), now.Add(time.Minute).UnixMilli(), now.Add(time.Minute).UnixMilli(), now.UnixMilli(), now.UnixMilli(), now.UnixMilli()}},
		{`INSERT INTO metadata_scrape_runs(id,game_id,game_content_revision_id,job_id,provider,provider_config_version,state,version,created_at_ms,updated_at_ms)
VALUES('provider-run','provider-game','provider-content','provider-job','HASHEOUS',1,'RUNNING',1,?,?)`, []any{now.UnixMilli(), now.UnixMilli()}},
		{`INSERT INTO content_hash_evidence(id,scrape_run_id,profile,blob_id,sha256,query_order,created_at_ms)
VALUES('provider-evidence','provider-run','RAW_FILE_V1','provider-busy',?,0,?)`, []any{busy.SHA256, now.UnixMilli()}},
		{`INSERT INTO metadata_provider_responses(id,provider,request_digest,http_status,outcome,raw_response_blob_id,raw_payload_state,fetched_at_ms,expires_at_ms)
VALUES('response-free','HASHEOUS',?,200,'HIT','provider-free','RETAINED',?,?)`, []any{free.SHA256, now.Add(-time.Hour).UnixMilli(), now.Add(-time.Minute).UnixMilli()}},
		{`INSERT INTO metadata_provider_responses(id,provider,request_digest,http_status,outcome,raw_response_blob_id,raw_payload_state,fetched_at_ms,expires_at_ms)
VALUES('response-busy','HASHEOUS',?,200,'HIT','provider-busy','RETAINED',?,?)`, []any{busy.SHA256, now.Add(-time.Hour).UnixMilli(), now.Add(-time.Minute).UnixMilli()}},
		{`INSERT INTO metadata_scrape_query_attempts(id,scrape_run_id,content_hash_evidence_id,provider_response_id,attempt_no,source,created_at_ms)
VALUES('provider-attempt','provider-run','provider-evidence','response-busy',1,'NETWORK',?)`, []any{now.UnixMilli()}},
	}
	for _, statement := range statements {
		_, err := transaction.ExecContext(t.Context(), statement.query, statement.args...)
		testassert.False(t, err != nil, err)
	}
	testassert.False(t, transaction.Commit() != nil)
}

func assertProviderPayloadState(t *testing.T, database *sql.DB, responseID, want string) {
	t.Helper()
	var state string
	var blobID *string
	testassert.False(t, database.QueryRowContext(t.Context(), `
SELECT raw_payload_state,raw_response_blob_id FROM metadata_provider_responses WHERE id=?
`, responseID).Scan(&state, &blobID) != nil)
	testassert.Falsef(t, state != want, "payload state = %s, want %s", state, want)
	testassert.Falsef(t, (blobID == nil) != (want == "RELEASED"), "payload blob = %v for state %s", blobID, want)
}

func digest64(character string) string {
	return character + "000000000000000000000000000000000000000000000000000000000000000"
}
