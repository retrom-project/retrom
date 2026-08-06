package blobgc

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/store"
)

func TestRunOnceHonorsGraceAndConcurrentReference(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	now := time.UnixMilli(1_786_000_000_000)
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	register := func(id, value string) blobstore.Metadata {
		t.Helper()
		metadata, err := blobs.Put(bytes.NewBufferString(value))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.SQL.Exec(`
INSERT INTO blobs(id,
sha256,
size_bytes,
md5,
sha1,
crc32,
media_type,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?)
`,
			id,
			metadata.SHA256,
			metadata.Size,
			metadata.MD5,
			metadata.SHA1,
			metadata.CRC32,
			"application/octet-stream",
			now.UnixMilli(),
		); err != nil {
			t.Fatal(err)
		}
		return metadata
	}
	orphan := register("orphan", "orphan")
	rescued := register("rescued", "rescued")
	protected := register("protected", "protected")
	if _, err := database.SQL.Exec(`
INSERT INTO metadata_provider_responses(id,
provider,
request_digest,
http_status,
outcome,
raw_response_blob_id,
fetched_at_ms,
expires_at_ms) VALUES('response',
'HASHEOUS',
?,
200,
'HIT',
'protected',
?,
?)
`, protected.SHA256, now.UnixMilli(), now.Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	service, err := New(database.SQL, blobs, func() time.Time { return now }, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.RunOnce(ctx)
	if err != nil || first.Scheduled != 2 || first.Deleted != 0 {
		t.Fatalf("first GC = %#v, error=%v", first, err)
	}
	if _, err := database.SQL.Exec(`
INSERT INTO metadata_provider_responses(id,
provider,
request_digest,
http_status,
outcome,
raw_response_blob_id,
fetched_at_ms,
expires_at_ms) VALUES('rescuer',
'HASHEOUS',
?,
200,
'HIT',
'rescued',
?,
?)
`, rescued.SHA256, now.UnixMilli(), now.Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(7*24*time.Hour + time.Millisecond)
	second, err := service.RunOnce(ctx)
	if err != nil || second.Deleted != 1 {
		t.Fatalf("second GC = %#v, error=%v", second, err)
	}
	if _, err := os.Stat(orphan.Path); !os.IsNotExist(err) {
		t.Fatalf("orphan still present: %v", err)
	}
	if _, err := os.Stat(rescued.Path); err != nil {
		t.Fatalf("rescued blob missing: %v", err)
	}
}
