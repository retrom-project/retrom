package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"retrom/internal/filestore"

	dbapi "retrom/internal/database"

	"github.com/google/uuid"

	"retrom/internal/testassert"
)

func TestGameCoverReplacementRetiresOldPayload(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	gameID, metadataID, contentID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	coverAssetID, videoAssetID := uuid.NewString(), uuid.NewString()
	coverFileRecord := ""
	transaction, err := server.database.BeginTx(t.Context(), nil)
	testassert.False(t, err != nil, err)
	defer dbapi.Rollback(transaction)
	fixture := gameDetailSeed{now: time.Now().UnixMilli()}
	seedGameDetailMedia(
		t, server, transaction, gameID, metadataID, contentID, coverFileRecord, coverAssetID, videoAssetID, &fixture,
	)
	mustCommitHTTPTest(t, transaction)
	oldAssetURL := "/content/assets/" + coverAssetID
	oldAsset := httptest.NewRecorder()
	server.Handler().ServeHTTP(oldAsset, httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, oldAssetURL, nil,
	))
	oldETag := oldAsset.Header().Get("ETag")
	testassert.Falsef(t, testassert.Any(
		func() bool { return oldAsset.Code != http.StatusOK },
		func() bool { return oldETag == "" },
		func() bool { return oldAsset.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" },
	), "old cover response = %d headers=%v", oldAsset.Code, oldAsset.Header())
	revalidatedRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, oldAssetURL, nil)
	revalidatedRequest.Header.Set("If-None-Match", oldETag)
	revalidated := httptest.NewRecorder()
	server.Handler().ServeHTTP(revalidated, revalidatedRequest)
	testassert.Falsef(t, revalidated.Code != http.StatusNotModified || revalidated.Body.Len() != 0,
		"old cover revalidation = %d body=%q", revalidated.Code, revalidated.Body.String())

	png, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	)
	testassert.False(t, err != nil, err)
	metadata, err := server.blobs.Put(bytes.NewReader(png))
	testassert.False(t, err != nil, err)
	newFileRecord, err := filestore.FileRecord(metadata, "image/png")
	testassert.False(t, err != nil, err)
	uploadID, uploadFileID := uuid.NewString(), uuid.NewString()
	mustExecHTTPTest(t, server.database, `
INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,version,
expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'COMPLETE','FILES',1,?,?,1,?,?,?)
`, uploadID, len(png), metadata.SHA256, fixture.now+60_000, fixture.now, fixture.now)
	mustCreateHTTPReferences(t, server.database, "upload_files", `
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,
final_file_record,state,created_at_ms,updated_at_ms)
VALUES(?,?,'replacement.png',?,?,?,'COMPLETE',?,?)
`, uploadFileID, uploadID, len(png), len(png), newFileRecord, fixture.now, fixture.now)

	response := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
		"/api/v1/admin/games/"+gameID+"/assets",
		strings.NewReader(`{"uploadFileId":"`+uploadFileID+`","kind":"COVER","ordinal":0}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"v1"`)
	request.Header.Set("Idempotency-Key", uuid.NewString())
	server.Handler().ServeHTTP(response, request)
	testassert.Falsef(t, response.Code != http.StatusCreated,
		"replace cover = %d %s", response.Code, response.Body.String())
	var created struct {
		AssetID string `json:"assetId"`
	}
	mustDecodeHTTPTest(t, response.Body.Bytes(), &created)
	newAssetURL := "/content/assets/" + created.AssetID
	newAsset := httptest.NewRecorder()
	server.Handler().ServeHTTP(newAsset, httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, newAssetURL, nil,
	))
	testassert.Falsef(t, testassert.Any(
		func() bool { return created.AssetID == "" || newAssetURL == oldAssetURL },
		func() bool { return newAsset.Code != http.StatusOK },
		func() bool { return newAsset.Header().Get("ETag") == "" || newAsset.Header().Get("ETag") == oldETag },
		func() bool { return newAsset.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" },
	), "replacement cover response = %d headers=%v body=%s", newAsset.Code, newAsset.Header(), response.Body.String())
	assertRetiredGameAssetUnavailable(t, server, coverAssetID)

	var directoryJobs int
	oldDirectory := filestore.GameDirectory(gameID) + "/media/" + coverAssetID
	mustScanHTTPTest(t, dbapi.QueryRowContext(t.Context(), server.database,
		`SELECT count(*) FROM job_input_snapshots WHERE json_extract(input_json,'$.inputs.relativePath')=?`, oldDirectory), &directoryJobs)
	testassert.Falsef(t, directoryJobs != 1, "expected one directory removal, got %d", directoryJobs)
	testassert.Falsef(t, !bytes.Equal(newAsset.Body.Bytes(), png), "replacement bytes changed")
}

func assertRetiredGameAssetUnavailable(t *testing.T, server *Server, assetID string) {
	t.Helper()
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/content/assets/"+assetID, nil,
	))
	testassert.Falsef(t, response.Code != http.StatusNotFound,
		"retired game asset %s remained available: %d %s", assetID, response.Code, response.Body.String())
}
