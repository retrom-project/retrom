package uploads

import (
	"bytes"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"retrom/internal/adapter/files/blobstore"
	uploadservice "retrom/internal/service/uploads"
)

func TestFinalizationClaimFreezesWorkerLeaseAndDeadline(t *testing.T) {
	fixture := newFinalizationFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	fixture.service = uploadservice.New(New(fixture.database), finalizationBlobs{put: func(reader io.Reader) (blobstore.Metadata, error) {
		close(entered)
		<-release
		return fixture.blobs.Put(reader)
	}}, fixture.root, finalizationNow)
	session := fixture.upload(t, []byte("bytes"))
	job := fixture.complete(t, session)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("finalizer did not enter CAS")
	}
	var worker sql.NullString
	var lease, deadline sql.NullInt64
	err := fixture.database.QueryRowContext(t.Context(), `SELECT worker_id,leased_until_ms,execution_deadline_at_ms FROM jobs WHERE id=?`, job).
		Scan(&worker, &lease, &deadline)
	close(release)
	awaitFinalizeState(t, fixture.database, job, "SUCCEEDED")
	if err != nil {
		t.Fatal(err)
	}
	now := finalizationNow().UnixMilli()
	if worker.String == "" || lease.Int64 != now+60000 || deadline.Int64 != now+(10*time.Minute).Milliseconds() {
		t.Fatalf("missing durable execution authority: worker=%v lease=%v deadline=%v", worker, lease, deadline)
	}
}

func TestFinalizationIOFailureAllowsSameJobRetry(t *testing.T) {
	fixture := newFinalizationFixture(t)
	failure := errors.New("temporary CAS write unavailable")
	fixture.service = uploadservice.New(New(fixture.database), finalizationBlobs{put: func(io.Reader) (blobstore.Metadata, error) {
		return blobstore.Metadata{}, failure
	}}, fixture.root, finalizationNow)
	job := fixture.complete(t, fixture.upload(t, []byte("bytes")))
	awaitFinalizeState(t, fixture.database, job, "FAILED")
	var retryable bool
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT error_retryable FROM jobs WHERE id=?`, job).Scan(&retryable); err != nil {
		t.Fatal(err)
	}
	if !retryable {
		t.Fatal("transient finalize I/O failure cannot use same Job retry")
	}
}

func TestFinalizationCorruptionRemovesOnlyFailedPart(t *testing.T) {
	fixture := newFinalizationFixture(t)
	data := append(bytes.Repeat([]byte{'x'}, int(uploadservice.PartSize)), []byte("bytes")...)
	session := fixture.upload(t, data)
	var key string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT storage_key FROM upload_parts WHERE upload_file_id=? AND part_no=1`, session.Files[0].ID).
		Scan(&key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.root, "tmp", "uploads", filepath.FromSlash(key)), []byte("wrong"), 0o600); err != nil {
		t.Fatal(err)
	}
	job := fixture.complete(t, session)
	awaitFinalizeState(t, fixture.database, job, "FAILED")
	current, err := fixture.service.Get(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	file := current.Files[0]
	if !reflect.DeepEqual(file.Parts, []int{0}) || file.Received != uploadservice.PartSize {
		t.Fatalf("bad part still received or good part lost: parts=%v received=%d", file.Parts, file.Received)
	}
	fixture.put(t, current, 1, []byte("bytes"))
	repaired, err := fixture.service.Get(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	next := fixture.complete(t, repaired)
	awaitFinalizeState(t, fixture.database, next, "SUCCEEDED")
	awaitFinalizeState(t, fixture.database, job, "FAILED")
}
