package payloadrelease

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

func TestImmediateGCSkipsRetentionAndUsesTheExistingWorker(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.UnixMilli(1_800_000_000_000)
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), func() time.Time { return now })
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close immediate GC database", database.Close()) })
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	metadata, err := blobs.Put(bytes.NewBufferString("immediate cleanup payload"))
	testassert.False(t, err != nil, err)
	_, err = database.SQL.ExecContext(ctx, `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('gc-profile','GC Admin',?);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES('gc-user','gc-profile','gc-admin','GC Admin','ADMIN','ENABLED',?,?);
`, now.UnixMilli(), now.UnixMilli(), now.UnixMilli())
	testassert.False(t, err != nil, err)
	_, err = database.SQL.ExecContext(ctx, `
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES('immediate-blob',?,?,?,?,?,?,?)
`, metadata.SHA256, metadata.Size, metadata.MD5, metadata.SHA1, metadata.CRC32,
		"application/octet-stream", now.UnixMilli())
	testassert.False(t, err != nil, err)
	service, err := New(database.SQL, blobs, func() time.Time { return now }, 7*24*time.Hour)
	testassert.False(t, err != nil, err)
	testassert.False(t, service.ReconcileGC(ctx) != nil)

	var retainedUntil int64
	testassert.False(t, database.SQL.QueryRowContext(ctx, `
SELECT job.available_at_ms FROM jobs job
JOIN blob_gc_candidates candidate ON candidate.gc_job_id=job.id
WHERE candidate.blob_id='immediate-blob'
`).Scan(&retainedUntil) != nil)
	testassert.Falsef(t, retainedUntil != now.Add(7*24*time.Hour).UnixMilli(),
		"retained until = %d", retainedUntil)

	result, err := service.ScheduleImmediateGC(ctx, "gc-user")
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, result.BlobCount != 1 || result.Bytes != metadata.Size || result.AcceptedAtMS != now.UnixMilli(),
		"immediate result = %#v", result)
	var availableAt, auditCount int64
	testassert.False(t, database.SQL.QueryRowContext(ctx, `
SELECT job.available_at_ms,
  (SELECT count(*) FROM audit_events WHERE action='STORAGE_CLEANUP_REQUESTED' AND actor_user_id='gc-user')
FROM jobs job JOIN blob_gc_candidates candidate ON candidate.gc_job_id=job.id
WHERE candidate.blob_id='immediate-blob'
`).Scan(&availableAt, &auditCount) != nil)
	testassert.Falsef(t, availableAt != now.UnixMilli() || auditCount != 1,
		"immediate scheduling = available:%d audits:%d", availableAt, auditCount)

	didWork, err := service.RunOnce(ctx)
	testassert.False(t, err != nil, err)
	testassert.True(t, didWork)
	var blobCount int64
	testassert.False(t, database.SQL.QueryRowContext(ctx, `SELECT count(*) FROM blobs WHERE id='immediate-blob'`).Scan(&blobCount) != nil)
	testassert.Falsef(t, blobCount != 0, "remaining Blob rows = %d", blobCount)
	_, statErr := os.Stat(blobs.Path(metadata.SHA256))
	testassert.True(t, os.IsNotExist(statErr))
}
