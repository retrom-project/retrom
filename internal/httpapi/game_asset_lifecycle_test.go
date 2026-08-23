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
	mustCommitHTTPTest(t, transaction)

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
	assertRetiredGameAssetUnavailable(t, server, coverAssetID)

	var retiredAssets, candidateCount int64
	mustScanHTTPTest(t, server.database.QueryRowContext(t.Context(), `
SELECT
 (SELECT count(*) FROM game_assets asset JOIN games game ON game.id=asset.game_id
  WHERE game.id=? AND asset.metadata_revision_id<>game.current_metadata_revision_id),
 (SELECT count(*) FROM blob_gc_candidates WHERE blob_id=?)
`, gameID, coverBlobID), &retiredAssets, &candidateCount)
	testassert.Falsef(t, retiredAssets != 0 || candidateCount != 1,
		"cover retirement = retired assets:%d GC candidates:%d", retiredAssets, candidateCount)
	if _, err := server.database.ExecContext(t.Context(), `DELETE FROM game_assets
WHERE game_id=? AND metadata_revision_id=(SELECT current_metadata_revision_id FROM games WHERE id=?)`, gameID, gameID); err == nil {
		t.Fatal("current game asset deletion was accepted")
	}

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
