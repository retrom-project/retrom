//go:build integration

package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	"retrom/internal/persistence/recordstore"
	libraryservice "retrom/internal/service/libraryimport"

	"github.com/google/uuid"
)

func createReviewVideoUpload(t *testing.T, server *testServer, payload []byte) string {
	t.Helper()
	blob, err := server.contentDeps.Files.Put(bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	value, err := filestore.FileRecord(blob, "video/mp4")
	if err != nil {
		t.Fatal(err)
	}
	uploadID, fileID := uuid.NewString(), uuid.NewString()
	mustExecHTTPTest(t, server.database, `INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,expires_at_ms,created_at_ms,updated_at_ms)
 VALUES(?,'COMPLETE','FILES',1,?,?,9999999999999,0,0)`, uploadID, blob.Size, blob.SHA256)
	_, err = recordstore.InsertRows(t.Context(), server.database, "upload_files", `INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,final_file_record,state,created_at_ms,updated_at_ms)
 VALUES(?,?,'review-video.mp4',?,?,?,'COMPLETE',0,0)`, fileID, uploadID, blob.Size, blob.Size, value)
	if err != nil {
		t.Fatal(err)
	}
	mustCreateHTTPReferences(t, server.database, "import_files", `INSERT INTO import_files(id,upload_session_id,relative_path,file_record,size_bytes,created_at_ms)
 SELECT id,upload_session_id,relative_path,final_file_record,received_size_bytes,created_at_ms FROM upload_files WHERE id=?`, fileID)
	return fileID
}

func requestReviewVideo(t *testing.T, server *testServer, itemID, fileID string, version int) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/reviews/"+itemID+"/assets", strings.NewReader(`{"uploadFileId":"`+fileID+`","kind":"VIDEO"}`))
	request.SetPathValue("importItemId", itemID)
	request.Header.Set("If-Match", fmt.Sprintf(`"v%d"`, version))
	request.Header.Set("Idempotency-Key", uuid.NewString())
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.createReviewAsset(response, request)
	return response
}

func selectReviewVideo(t *testing.T, server *testServer, itemID, videoID string, version int, want int) {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/v1/admin/reviews/"+itemID, strings.NewReader(`{"selectedAssets":{"coverCandidateAssetId":null,"coverUploadedAssetId":null,"videoUploadedAssetId":"`+videoID+`","backgroundCandidateAssetId":null,"screenshotCandidateAssetIds":[]},"tagIds":[]}`))
	request.SetPathValue("importItemId", itemID)
	request.Header.Set("If-Match", fmt.Sprintf(`"v%d"`, version))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.patchReview(response, request)
	if response.Code != want {
		t.Fatalf("select video = %d %s, want %d", response.Code, response.Body.String(), want)
	}
}

func TestReviewVideoUploadReplaceAndPublish(t *testing.T) {
	server := newTestServer(t)
	itemID := createReviewSnapshotItem(t, server)
	original := []byte{0, 0, 0, 12, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}
	first := uploadReviewVideo(t, server, itemID, original, 1)
	selectReviewVideo(t, server, itemID, first.AssetID, 1, http.StatusOK)
	replacement := append(append([]byte{}, original...), []byte("replacement")...)
	second := uploadReviewVideo(t, server, itemID, replacement, 2)
	selectReviewVideo(t, server, itemID, second.AssetID, 2, http.StatusOK)
	assertReviewVideoDetail(t, server, itemID, second.AssetID)
	approved, err := server.importDeps.Importer.Approve(t.Context(), itemID, 3)
	if err != nil {
		t.Fatal(err)
	}
	var record string
	var count int
	err = dbapi.QueryRowContext(t.Context(), server.database, `SELECT count(*),file_record FROM game_assets WHERE game_id=? AND kind='VIDEO'`, approved.GameID).Scan(&count, &record)
	if err != nil || count != 1 {
		t.Fatalf("published videos = %d, err=%v", count, err)
	}
	saved, err := filestore.ParseRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	var source string
	err = dbapi.QueryRowContext(t.Context(), server.database, `SELECT file_record FROM review_uploaded_assets WHERE id=?`, second.AssetID).Scan(&source)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := filestore.ParseRecord(source)
	if err != nil || saved.SHA256 != expected.SHA256 || record == source {
		t.Fatalf("published video is not an independent replacement copy: %v", err)
	}
}

func uploadReviewVideo(t *testing.T, server *testServer, itemID string, contents []byte, version int) libraryservice.ReviewAssetResult {
	t.Helper()
	fileID := createReviewVideoUpload(t, server, contents)
	response := requestReviewVideo(t, server, itemID, fileID, version)
	if response.Code != http.StatusCreated {
		t.Fatalf("video upload = %d %s", response.Code, response.Body.String())
	}
	var result libraryservice.ReviewAssetResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Kind != "VIDEO" || result.Width != nil || result.Height != nil || result.Version != int64(version) {
		t.Fatalf("video contract = %+v", result)
	}
	replay := requestReviewVideo(t, server, itemID, fileID, version)
	if replay.Code != http.StatusCreated || !bytes.Equal(replay.Body.Bytes(), response.Body.Bytes()) {
		t.Fatalf("video replay = %d %s", replay.Code, replay.Body.String())
	}
	return result
}

func assertReviewVideoDetail(t *testing.T, server *testServer, itemID, assetID string) {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/reviews/"+itemID, nil)
	request.SetPathValue("importItemId", itemID)
	response := httptest.NewRecorder()
	server.review(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"videoUploadedAssetId":"`+assetID+`"`) || !strings.Contains(response.Body.String(), `"widthPx":null,"heightPx":null`) {
		t.Fatalf("video detail = %d %s", response.Code, response.Body.String())
	}
}

func TestReviewVideoRejectsInvalidSelectionAndUpload(t *testing.T) {
	server := newTestServer(t)
	itemID := createReviewSnapshotItem(t, server)
	coverID := createReviewAssetFixture(t, server, itemID, createReviewAssetUpload(t, server))
	selectReviewVideo(t, server, itemID, coverID, 1, http.StatusUnprocessableEntity)
	invalidID := createReviewVideoUpload(t, server, []byte("disguised image"))
	invalid := requestReviewVideo(t, server, itemID, invalidID, 1)
	if invalid.Code != http.StatusUnprocessableEntity || !strings.Contains(invalid.Body.String(), "ASSET_VIDEO_INVALID") {
		t.Fatalf("invalid video = %d %s", invalid.Code, invalid.Body.String())
	}
	valid := uploadReviewVideo(t, server, itemID, []byte{0, 0, 0, 12, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, 1)
	other := createReviewSnapshotItem(t, server)
	selectReviewVideo(t, server, other, valid.AssetID, 1, http.StatusUnprocessableEntity)
	selectReviewVideo(t, server, itemID, valid.AssetID, 1, http.StatusOK)
	selectReviewVideo(t, server, itemID, valid.AssetID, 1, http.StatusConflict)
	assertReviewVideoDetail(t, server, itemID, valid.AssetID)
}
