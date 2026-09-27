package blobregistry

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/store"
	"retrom/internal/testassert"
)

func TestRegistryExactlyCoversBlobForeignKeys(t *testing.T) {
	t.Parallel()
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	if err := ValidateSchema(context.Background(), database.SQL); err != nil {
		t.Fatal(err)
	}
}

func TestReferenceCountsTrackOwnersAndArchiveMembers(t *testing.T) {
	t.Parallel()
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	for index, id := range []string{"archive", "member", "other"} {
		value := index + 1
		mustReferenceCountExec(t, database.SQL, `INSERT INTO blobs
(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES(?,?,?,?,?,?,?,1)`, id, fmt.Sprintf("%064x", value), 1,
			fmt.Sprintf("%032x", value), fmt.Sprintf("%040x", value), fmt.Sprintf("%08x", value), "application/octet-stream")
	}
	mustReferenceInsert(t, database.SQL, "archive_entries", `INSERT INTO archive_entries
(archive_blob_id,ordinal,original_relative_path,normalized_path,ascii_casefold_path,
archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,materialized_blob_id,created_at_ms)
VALUES('archive',0,'member','member','member','ZIP','STORE',1,?,?,?,?, 'member',1)`,
		strings.Repeat("1", 8), strings.Repeat("1", 32), strings.Repeat("1", 40), strings.Repeat("1", 64))
	mustReferenceInsert(t, database.SQL, "archive_entries", `INSERT INTO archive_entries
(archive_blob_id,ordinal,original_relative_path,normalized_path,ascii_casefold_path,
archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,materialized_blob_id,created_at_ms)
VALUES('archive',1,'self','self','self','ZIP','STORE',1,?,?,?,?, 'archive',1)`,
		strings.Repeat("2", 8), strings.Repeat("2", 32), strings.Repeat("2", 40), strings.Repeat("2", 64))
	check := func(archive, member, other int) {
		t.Helper()
		var a, m, o int
		if err := dbapi.QueryRowContext(t.Context(), database.SQL, `SELECT
(SELECT ref_count FROM blobs WHERE id='archive'),
(SELECT ref_count FROM blobs WHERE id='member'),
(SELECT ref_count FROM blobs WHERE id='other')`).Scan(&a, &m, &o); err != nil {
			t.Fatal(err)
		}
		if a != archive || m != member || o != other {
			t.Fatalf("reference counts = %d/%d/%d, want %d/%d/%d", a, m, o, archive, member, other)
		}
	}
	check(0, 0, 0)
	mustReferenceInsert(t, database.SQL, "metadata_provider_responses", `INSERT INTO metadata_provider_responses
(id,provider,request_digest,http_status,outcome,raw_response_blob_id,raw_payload_state,fetched_at_ms,expires_at_ms)
VALUES('owner','HASHEOUS',?,200,'HIT','archive','RETAINED',1,2)`, strings.Repeat("a", 64))
	check(1, 1, 0)
	mustReferenceInsert(t, database.SQL, "metadata_provider_responses", `INSERT INTO metadata_provider_responses
(id,provider,request_digest,http_status,outcome,raw_response_blob_id,raw_payload_state,fetched_at_ms,expires_at_ms)
VALUES('second-owner','HASHEOUS',?,200,'HIT','archive','RETAINED',1,2)`, strings.Repeat("b", 64))
	check(2, 1, 0)
	if _, err := recordstore.UpdateReferences(t.Context(), database.SQL, "metadata_provider_responses", recordstore.Update{Set: "raw_response_blob_id='other'", Scope: recordstore.Scope{Where: "id='owner'"}}); err != nil {
		t.Fatal(err)
	}
	check(1, 1, 1)
	if _, err := recordstore.DeleteReferences(t.Context(), database.SQL, "metadata_provider_responses", recordstore.Scope{Where: "id='second-owner'"}); err != nil {
		t.Fatal(err)
	}
	check(0, 0, 1)
	tx, err := database.SQL.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recordstore.DeleteReferences(t.Context(), tx, "metadata_provider_responses", recordstore.Scope{Where: "id='owner'"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	check(0, 0, 1)
}

func mustReferenceCountExec(t *testing.T, database dbapi.Executor, query string, args ...any) {
	t.Helper()
	if _, err := database.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func mustReferenceInsert(t *testing.T, database dbapi.Executor, table, query string, args ...any) {
	t.Helper()
	if _, err := recordstore.CreateReferences(t.Context(), database, table, query, args...); err != nil {
		t.Fatal(err)
	}
}
