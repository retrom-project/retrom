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

func TestReferenceCountTriggersMatchOwningEdges(t *testing.T) {
	t.Parallel()
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	edges, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, edge := range edges {
		if edge.Class != "PROTECTIVE" {
			continue
		}
		for _, operation := range []string{"insert", "delete", "update"} {
			name := fmt.Sprintf("count_%s_%s_%s", edge.Table, edge.Column, operation)
			var count int
			if err := dbapi.QueryRowContext(t.Context(), database.SQL,
				`SELECT count(*) FROM sqlite_schema WHERE type='trigger' AND name=?`, name).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Errorf("missing reference count trigger %s", name)
			}
		}
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
		_, err := database.SQL.ExecContext(t.Context(), `INSERT INTO blobs
(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES(?,?,?,?,?,?,?,1)`, id, fmt.Sprintf("%064x", value), 1,
			fmt.Sprintf("%032x", value), fmt.Sprintf("%040x", value), fmt.Sprintf("%08x", value), "application/octet-stream")
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = database.SQL.ExecContext(t.Context(), `INSERT INTO archive_entries
(archive_blob_id,ordinal,original_relative_path,normalized_path,ascii_casefold_path,
archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,materialized_blob_id,created_at_ms)
VALUES('archive',0,'member','member','member','ZIP','STORE',1,?,?,?,?, 'member',1)`,
		strings.Repeat("1", 8), strings.Repeat("1", 32), strings.Repeat("1", 40), strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
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
	_, err = database.SQL.ExecContext(t.Context(), `INSERT INTO metadata_provider_responses
(id,provider,request_digest,http_status,outcome,raw_response_blob_id,raw_payload_state,fetched_at_ms,expires_at_ms)
VALUES('owner','HASHEOUS',?,200,'HIT','archive','RETAINED',1,2)`, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	check(1, 1, 0)
	_, err = database.SQL.ExecContext(t.Context(), `UPDATE metadata_provider_responses
SET raw_response_blob_id='other' WHERE id='owner'`)
	if err != nil {
		t.Fatal(err)
	}
	check(0, 0, 1)
	tx, err := database.SQL.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), `DELETE FROM metadata_provider_responses WHERE id='owner'`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	check(0, 0, 1)
}
