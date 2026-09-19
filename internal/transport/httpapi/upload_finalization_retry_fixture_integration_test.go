//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	blobmodel "retrom/internal/model/blob"
	uploadsmodel "retrom/internal/model/uploads"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/integration/libraryimport"
	jobpersistence "retrom/internal/repo/jobs"
	uploadpersistence "retrom/internal/repo/uploads"
	"retrom/internal/service/jobs"
	"retrom/internal/service/uploads"
	"retrom/internal/testkit/testsupport"
)

type uploadRetryFixture struct {
	validationRetryFixture
	version int64
}
type retryUploadBlobs struct {
	blobs *blobstore.Store
	calls atomic.Int64
}

func (source *retryUploadBlobs) Put(reader io.Reader) (blobmodel.PreparedBlob, error) {
	if source.calls.Add(1) == 1 {
		return blobmodel.PreparedBlob{}, errors.New("temporary upload CAS failure")
	}
	return source.blobs.Put(reader)
}

func newUploadRetryFixture(t *testing.T) uploadRetryFixture {
	t.Helper()
	now := func() time.Time { return time.Date(2028, 3, 4, 5, 6, 7, 0, time.UTC) }
	root := t.TempDir()
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(root, "retrom.db"), now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	blobs, err := blobstore.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	uploader := uploads.New(uploadpersistence.New(database.SQL), &retryUploadBlobs{blobs: blobs}, root, now)
	t.Cleanup(uploader.Close)
	server := &Server{
		database: database.SQL, now: now, uploads: uploader, jobService: jobs.New(jobpersistence.New(database.SQL), now),
		importer: libraryimport.New(database.SQL, now),
	}
	server.idempotencyQueueDrained = sync.NewCond(&server.idempotencyQueueMu)
	session, err := uploader.Create(t.Context(), uploadsmodel.CreateRequest{SourceType: "FILES", Files: []uploadsmodel.FileDeclaration{
		{ClientFileID: "fixture", RelativePath: "fixture.bin", SizeBytes: 5},
	}})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("bytes"))
	digest := "sha-256=:" + base64.StdEncoding.EncodeToString(sum[:]) + ":"
	if err := uploader.PutPart(t.Context(), session.ID, session.Files[0].ID, 0, "bytes 0-4/5", digest, bytes.NewBufferString("bytes")); err != nil {
		t.Fatal(err)
	}
	current, err := uploader.Get(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := uploader.Complete(t.Context(), session.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	fixture := uploadRetryFixture{validationRetryFixture: validationRetryFixture{server: server, jobID: id, now: now}}
	state, _, _ := waitValidationRetry(t, fixture.validationRetryFixture)
	if state != "FAILED" {
		t.Fatalf("initial upload=%s", state)
	}
	if err := database.SQL.QueryRowContext(t.Context(), `SELECT version FROM jobs WHERE id=?`, id).Scan(&fixture.version); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (fixture uploadRetryFixture) request(ctx context.Context, writer http.ResponseWriter) {
	ctx = context.WithValue(ctx, operationIDContextKey, "postAdminJobRetry")
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/admin/jobs/"+fixture.jobID+"/retry", strings.NewReader(`{}`))
	request.SetPathValue("jobId", fixture.jobID)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", validationRetryKey)
	request.Header.Set("If-Match", fmt.Sprintf(`"v%d"`, fixture.version))
	fixture.server.idempotencyHandler(http.HandlerFunc(fixture.server.retryJob)).ServeHTTP(writer, request)
}
