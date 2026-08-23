package payloadrelease

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

func TestInterruptedConsumptionReleaseRestartsIdempotently(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.UnixMilli(1_786_000_000_000)
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), func() time.Time { return now })
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	metadata, err := blobs.Put(bytes.NewBufferString("interrupted upload"))
	testassert.False(t, err != nil, err)
	_, err = database.SQL.ExecContext(ctx, `
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES('restart-blob',?,?,?,?,?,?,?)
`, metadata.SHA256, metadata.Size, metadata.MD5, metadata.SHA1, metadata.CRC32,
		"application/octet-stream", now.UnixMilli())
	testassert.False(t, err != nil, err)
	transaction, err := database.SQL.BeginTx(ctx, nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	_, err = transaction.ExecContext(ctx, `
INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,version,expires_at_ms,created_at_ms,updated_at_ms)
VALUES('restart-upload','COMPLETE','FILES',1,?, ?,1,?,?,?)
`, metadata.Size, digest64("3"), now.Add(time.Hour).UnixMilli(), now.UnixMilli(), now.UnixMilli())
	testassert.False(t, err != nil, err)
	_, err = transaction.ExecContext(ctx, `
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,final_blob_id,state,created_at_ms,updated_at_ms)
VALUES('restart-file','restart-upload','fixture.bin',?,?,'restart-blob','COMPLETE',?,?)
`, metadata.Size, metadata.Size, now.UnixMilli(), now.UnixMilli())
	testassert.False(t, err != nil, err)
	_, err = transaction.ExecContext(ctx, `
INSERT INTO upload_consumptions(id,upload_session_id,upload_file_id,consumer_type,consumer_id,version,created_at_ms)
VALUES('restart-consumption','restart-upload','restart-file','GAME_ASSET','restart-owner',1,?)
`, now.UnixMilli())
	testassert.False(t, err != nil, err)
	jobID, err := ScheduleConsumption(ctx, transaction, "restart-consumption", now.UnixMilli())
	testassert.False(t, err != nil, err)
	testassert.False(t, transaction.Commit() != nil)

	service, err := New(database.SQL, blobs, func() time.Time { return now }, 7*24*time.Hour)
	testassert.False(t, err != nil, err)
	claimed, found, err := service.claim(ctx)
	testassert.False(t, err != nil, err)
	testassert.True(t, found)
	testassert.Falsef(t, claimed.ID != jobID, "claimed job = %s, want %s", claimed.ID, jobID)
	testassert.False(t, service.recoverInterruptedJobs(ctx) != nil)

	didWork, err := service.RunOnce(ctx)
	testassert.False(t, err != nil, err)
	testassert.True(t, didWork)
	var jobState, fileState string
	var blobID *string
	testassert.False(t, database.SQL.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=?`, jobID).Scan(&jobState) != nil)
	testassert.Falsef(t, jobState != "SUCCEEDED", "job state = %s", jobState)
	testassert.False(t, database.SQL.QueryRowContext(ctx, `SELECT state,final_blob_id FROM upload_files WHERE id='restart-file'`).Scan(&fileState, &blobID) != nil)
	testassert.Falsef(t, fileState != "PURGED" || blobID != nil, "upload file = %s, blob = %v", fileState, blobID)
	var candidateCount int
	testassert.False(t, database.SQL.QueryRowContext(ctx, `SELECT count(*) FROM blob_gc_candidates WHERE blob_id='restart-blob'`).Scan(&candidateCount) != nil)
	testassert.Falsef(t, candidateCount != 1, "candidate count = %d", candidateCount)

	// Reconciliation and a second dispatcher pass cannot recreate the release
	// Job or collect the Blob before the configured grace period.
	testassert.False(t, service.ReconcileGC(ctx) != nil)
	didWork, err = service.RunOnce(ctx)
	testassert.False(t, err != nil, err)
	testassert.False(t, didWork)
	var releaseJobCount int
	testassert.False(t, database.SQL.QueryRowContext(ctx, `
SELECT count(*) FROM jobs WHERE kind='PAYLOAD_RELEASE' AND scope_type='UPLOAD_CONSUMPTION' AND scope_id='restart-consumption'
`).Scan(&releaseJobCount) != nil)
	testassert.Falsef(t, releaseJobCount != 1, "release Job count = %d", releaseJobCount)
}
