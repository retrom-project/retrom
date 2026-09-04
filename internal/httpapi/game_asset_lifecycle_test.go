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

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/storageanalysis"
	"retrom/internal/testassert"
)

func TestGameCoverReplacementRetiresOldPayloadAndStagesCapacity(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	gameID, metadataID, contentID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	coverBlobID, coverAssetID, videoAssetID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	transaction, err := server.database.BeginTx(t.Context(), nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	fixture := gameDetailSeed{now: time.Now().UnixMilli()}
	seedGameDetailMedia(
		t, server, transaction, gameID, metadataID, contentID, coverBlobID, coverAssetID, videoAssetID, &fixture,
	)
	oldCoverMetadata, err := server.blobs.Put(bytes.NewReader([]byte("old cover payload")))
	testassert.False(t, err != nil, err)
	mustExecHTTPTest(t, transaction, `
UPDATE blobs SET sha256=?,size_bytes=?,md5=?,sha1=?,crc32=? WHERE id=?
`, oldCoverMetadata.SHA256, oldCoverMetadata.Size, oldCoverMetadata.MD5, oldCoverMetadata.SHA1,
		oldCoverMetadata.CRC32, coverBlobID)
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
	newBlobID, err := blobstore.EnsureRecord(t.Context(), server.database, metadata, "image/png", fixture.now)
	testassert.False(t, err != nil, err)
	uploadID, uploadFileID := uuid.NewString(), uuid.NewString()
	mustExecHTTPTest(t, server.database, `
INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,version,expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'COMPLETE','FILES',1,?,?,1,?,?,?)
`, uploadID, len(png), metadata.SHA256, fixture.now+60_000, fixture.now, fixture.now)
	mustExecHTTPTest(t, server.database, `
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,final_blob_id,state,created_at_ms,updated_at_ms)
VALUES(?,?,'replacement.png',?,?,?,'COMPLETE',?,?)
`, uploadFileID, uploadID, len(png), len(png), newBlobID, fixture.now, fixture.now)

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

	var retiredAssets, candidateCount int64
	mustScanHTTPTest(t, server.database.QueryRowContext(t.Context(), `
SELECT
 (SELECT count(*) FROM game_assets asset JOIN games game ON game.id=asset.game_id
  WHERE game.id=? AND asset.game_id<>game.id),
 (SELECT count(*) FROM blob_gc_candidates WHERE blob_id=?)
`, gameID, coverBlobID), &retiredAssets, &candidateCount)
	testassert.Falsef(t, retiredAssets != 0 || candidateCount != 1,
		"cover retirement = retired assets:%d GC candidates:%d", retiredAssets, candidateCount)
	snapshot, err := storageanalysis.New(server.database, time.Now).Analyze(t.Context())
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, snapshot.Totals.UnreferencedBytes < 4,
		"unreferenced bytes = %d, wanted retired cover bytes", snapshot.Totals.UnreferencedBytes)
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
