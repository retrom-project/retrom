package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	"retrom/internal/dependencies"
	"retrom/internal/httpapi/generated"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/store"
)

func TestHealthAndWritesDoNotRequireCSRF(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)

	live := httptest.NewRecorder()
	server.Handler().ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if live.Code != http.StatusOK || live.Header().Get("X-Request-ID") == "" {
		t.Fatalf("live status = %d, request id = %q", live.Code, live.Header().Get("X-Request-ID"))
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/platform-instances",
		strings.NewReader(`{"platformId":"gbc","defaultCoreId":"gambatte","name":"No CSRF","description":"","sortOrder":900}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", uuid.NewString())
	created := httptest.NewRecorder()
	server.Handler().ServeHTTP(created, request)
	if created.Code != http.StatusCreated {
		t.Fatalf("write without CSRF status = %d %s", created.Code, created.Body.String())
	}
}

func TestWritesIgnoreBrowserOriginWithoutEnablingCORS(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	handler := server.Handler()
	send := func(name string, headers map[string]string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(
			`{"platformId":"gbc","defaultCoreId":"gambatte","name":%q,"description":"","sortOrder":900}`,
			"LAN "+name,
		)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/platform-instances", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", uuid.NewString())
		for name, value := range headers {
			request.Header.Set(name, value)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("CORS header unexpectedly present: %q", recorder.Header().Get("Access-Control-Allow-Origin"))
		}
		return recorder
	}
	if response := send("no-origin", nil); response.Code != http.StatusCreated {
		t.Fatalf("request without origin = %d %s", response.Code, response.Body.String())
	}
	crossOrigin := send("cross-origin", map[string]string{
		"Origin":         "https://external.example",
		"Sec-Fetch-Site": "cross-site",
	})
	if crossOrigin.Code != http.StatusCreated {
		t.Fatalf("cross-origin LAN write = %d %s", crossOrigin.Code, crossOrigin.Body.String())
	}
}

func TestRuntimeAllowlistRejectsUnknownPath(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	recorder := httptest.NewRecorder()
	server.Handler().
		ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/runtime/emulatorjs/4.2.3/not-in-manifest.js", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown runtime path status = %d", recorder.Code)
	}
}

func TestRestrictedBinaryEndpointsRejectMultipleRanges(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodGet, "/runtime/launches/id/game/game.zip", nil)
	request.Header.Set("Range", "bytes=0-1,4-5")
	recorder := httptest.NewRecorder()
	if !rejectMultipleRanges(recorder, request) || recorder.Code != http.StatusRequestedRangeNotSatisfiable ||
		recorder.Header().Get("Content-Range") != "" ||
		!strings.Contains(recorder.Body.String(), `"code":"MULTIPLE_RANGES_UNSUPPORTED"`) {
		t.Fatalf("multiple range response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestDiagnosticsUsesClosedSnapshotSchemaAndRequiredHeaders(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	fixed := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	server.now = func() time.Time { return fixed }
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/diagnostics", nil))
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "application/json; charset=utf-8" ||
		recorder.Header().Get("Cache-Control") != "private, no-store" ||
		recorder.Header().Get("Content-Disposition") != `attachment; filename="retrom-diagnostics.json"` ||
		recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf(
			"diagnostics response = %d headers=%v body=%s",
			recorder.Code,
			recorder.Header(),
			recorder.Body.String(),
		)
	}
	var response struct {
		SchemaVersion         int64 `json:"schemaVersion"`
		GeneratedAtMS         int64 `json:"generatedAtMs"`
		DatabaseSchemaVersion int64 `json:"databaseSchemaVersion"`
		Dependencies          struct {
			Configured []string `json:"configuredEmulatorjsVersions"`
			Active     string   `json:"activeEmulatorjsVersion"`
		} `json:"dependencies"`
		Counts struct {
			Games struct {
				Published int64 `json:"published"`
				Deleted   int64 `json:"deleted"`
			} `json:"games"`
			SaveStates struct {
				Active  int64 `json:"active"`
				Deleted int64 `json:"deleted"`
			} `json:"saveStates"`
			Blobs int64 `json:"blobs"`
			Jobs  struct {
				Queued          int64 `json:"queued"`
				Running         int64 `json:"running"`
				CancelRequested int64 `json:"cancelRequested"`
				Succeeded       int64 `json:"succeeded"`
				Failed          int64 `json:"failed"`
				Cancelled       int64 `json:"cancelled"`
			} `json:"jobs"`
			DATVersions struct {
				Pending   int64 `json:"pending"`
				Parsing   int64 `json:"parsing"`
				Ready     int64 `json:"ready"`
				Failed    int64 `json:"failed"`
				Cancelled int64 `json:"cancelled"`
			} `json:"datVersions"`
		} `json:"counts"`
	}
	decoder := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("diagnostics schema: %v: %s", err, recorder.Body.String())
	}
	if response.SchemaVersion != 1 || response.GeneratedAtMS != fixed.UnixMilli() ||
		response.DatabaseSchemaVersion != 14 ||
		!slices.Equal(response.Dependencies.Configured, []string{"4.2.3"}) ||
		response.Dependencies.Active != "4.2.3" {
		t.Fatalf("diagnostics values = %#v", response)
	}
}

func TestBlockedReviewDetailRemainsVisibleWithoutSelectedValidation(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	now := time.Now()
	if err := server.dependencies.Bootstrap(context.Background(), server.database, now); err != nil {
		t.Fatal(err)
	}
	var artifactID string
	if err := server.database.QueryRow(`
SELECT id
FROM core_artifacts
WHERE core_id='mgba'
AND enabled=1
`).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	itemID := "01980000-0000-7000-8000-000000000121"
	importID := "01980000-0000-7000-8000-000000000122"
	uploadID := "01980000-0000-7000-8000-000000000123"
	validationID := "01980000-0000-7000-8000-000000000124"
	draftID := "01980000-0000-7000-8000-000000000125"
	scrapeJobID := "01980000-0000-7000-8000-000000000126"
	scrapeRunID := "01980000-0000-7000-8000-000000000127"
	providerResponseID := "01980000-0000-7000-8000-000000000128"
	candidateID := "01980000-0000-7000-8000-000000000129"
	candidateAssetID := "01980000-0000-7000-8000-000000000130"
	readyCoverAssetID := "01980000-0000-7000-8000-000000000131"
	sourceBlobID := "01980000-0000-7000-8000-000000000132"
	coverBlobID := "01980000-0000-7000-8000-000000000133"
	uploadFileID := "01980000-0000-7000-8000-000000000134"
	coverUploadFileID := "01980000-0000-7000-8000-000000000136"
	digest := strings.Repeat("a", 64)
	timestamp := now.UnixMilli()
	coverPayload, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	coverMetadata, err := server.blobs.Put(bytes.NewReader(coverPayload))
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := server.database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.Exec(`
INSERT INTO upload_sessions(id,
state,
source_type,
total_files,
total_bytes,
manifest_digest,
version,
expires_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
'COMPLETE',
'FILES',
1,
0,
?,
1,
?,
?,
?)
`, uploadID, digest, timestamp+60_000, timestamp, timestamp); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO import_jobs(id,
upload_session_id,
target_platform_instance_id,
platform_instance_version,
platform_id,
default_core_id,
core_artifact_id,
metadata_provider,
config_snapshot_json,
config_snapshot_digest,
state,
total_item_count,
review_pending_item_count,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
'01980000-0000-7000-8000-000000000005',
1,
'gba',
'mgba',
?,
'HASHEOUS',
'{}',
?,
'REVIEW_PENDING',
1,
1,
1,
?,
?)
`, importID, uploadID, artifactID, digest, timestamp, timestamp); err != nil {
		t.Fatal(err)
	}
	manifest := `{"files":[{"logicalName":"blocked.gba","role":"CONTENT"}]}`
	if _, err := transaction.Exec(`
INSERT INTO import_items(id,
import_job_id,
group_key,
state,
source_manifest_json,
source_manifest_digest,
search_text,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
'REVIEW_PENDING',
?,
?,
'blocked.gba',
1,
?,
?)
`, itemID, importID, digest, manifest, digest, timestamp, timestamp); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms) VALUES
(?,?,4096,?,?,?,'application/octet-stream',?),
(?,?,?,?,?,?,'image/png',?)
`, sourceBlobID, strings.Repeat("b", 64), strings.Repeat("c", 32), strings.Repeat("d", 40), strings.Repeat("e", 8), timestamp,
		coverBlobID, coverMetadata.SHA256, coverMetadata.Size, coverMetadata.MD5, coverMetadata.SHA1, coverMetadata.CRC32, timestamp); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,final_blob_id,state,created_at_ms,updated_at_ms)
VALUES(?,?, 'blocked.zip',4096,4096,?,'COMPLETE',?,?),
(?,?,'manual-cover.png',?,?,?,'COMPLETE',?,?)
	`, uploadFileID, uploadID, sourceBlobID, timestamp, timestamp,
		coverUploadFileID, uploadID, coverMetadata.Size, coverMetadata.Size, coverBlobID, timestamp, timestamp); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO archive_entries(archive_blob_id,ordinal,original_relative_path,normalized_path,ascii_casefold_path,
archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,materialized_blob_id,created_at_ms)
VALUES(?,0,'blocked.gba','blocked.gba','blocked.gba','ZIP','DEFLATE',4096,?,?,?,?,?,?)
	`, sourceBlobID, strings.Repeat("e", 8), strings.Repeat("c", 32), strings.Repeat("d", 40), strings.Repeat("b", 64), sourceBlobID, timestamp); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO import_item_source_files(import_item_id,role,logical_name,upload_file_id,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order,created_at_ms)
VALUES(?,'CONTENT','blocked.zip',?,?,NULL,NULL,0,?)
	`, itemID, uploadFileID, sourceBlobID, timestamp); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO import_item_core_validations(id,
import_item_id,
target_platform_instance_id,
platform_instance_version,
core_id,
core_artifact_id,
source_manifest_digest,
prepublish_input_digest,
status,
compatibility_code,
dependency_snapshot_json,
created_at_ms) VALUES(?,
?,
'01980000-0000-7000-8000-000000000005',
1,
'mgba',
?,
?,
?,
'BLOCKED',
'DEPENDENCY_MISSING',
'{"dependencies":[]}',
?)
`, validationID, itemID, artifactID, digest, digest, timestamp); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO review_drafts(id,
import_item_id,
target_platform_instance_id,
selected_validation_id,
metadata_json,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
'01980000-0000-7000-8000-000000000005',
NULL,
'{"title":"Blocked","description":"","developer":"","publisher":"","genre":"","players":null,"releaseYear":null}',
1,
?,
?)
`, draftID, itemID, timestamp, timestamp); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO jobs(id,
scope_type,
scope_id,
kind,
dedupe_key,
execution_no,
payload_json,
cancellable,
state,
attempt_count,
max_attempts,
version,
available_at_ms,
finished_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
'IMPORT_ITEM',
?,
'METADATA_SCRAPE',
?,
1,
'{}',
0,
'SUCCEEDED',
1,
1,
1,
?,
?,
?,
?)
`, scrapeJobID, itemID, digest, timestamp, timestamp, timestamp, timestamp); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO metadata_scrape_runs(id,
import_item_id,
job_id,
provider,
provider_config_version,
state,
version,
created_at_ms,
updated_at_ms,
completed_at_ms) VALUES(?,
?,
?,
'HASHEOUS',
1,
'COMPLETED',
1,
?,
?,
?)
`, scrapeRunID, itemID, scrapeJobID, timestamp, timestamp, timestamp); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO metadata_provider_responses(id,
provider,
request_digest,
http_status,
outcome,
fetched_at_ms,
expires_at_ms) VALUES(?,
'HASHEOUS',
?,
200,
'HIT',
?,
?)
`, providerResponseID, digest, timestamp, timestamp); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO scrape_candidates(id,
scrape_run_id,
primary_response_id,
provider_game_id,
normalized_metadata_json,
evidence_json,
created_at_ms) VALUES(?,
?,
?,
'73',
'{"title":"Visible candidate"}',
'{}',
?)
`, candidateID, scrapeRunID, providerResponseID, timestamp); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO scrape_candidate_assets(id,
scrape_candidate_id,
provider_response_id,
provider_asset_id,
kind_hint,
ordinal,
source_path,
status,
error_code,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
'cover',
'COVER',
0,
'/api/v1/images/cover',
'FAILED',
'ASSET_HTTP_STATUS',
1,
?,
?)
`, candidateAssetID, candidateID, providerResponseID, timestamp, timestamp); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO scrape_candidate_assets(id,scrape_candidate_id,provider_response_id,provider_asset_id,kind_hint,ordinal,
source_path,status,blob_id,width_px,height_px,media_type,fetched_at_ms,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,'cover-ready','COVER',1,'/api/v1/images/cover-ready','READY',?,600,800,'image/png',?,1,?,?)
`, readyCoverAssetID, candidateID, providerResponseID, coverBlobID, timestamp, timestamp, timestamp); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reviews/"+itemID, nil)
	request.SetPathValue("importItemId", itemID)
	server.review(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"status":"BLOCKED"`) ||
		!strings.Contains(recorder.Body.String(), `"current":false`) ||
		!strings.Contains(recorder.Body.String(), `"compatibilityCode":"DEPENDENCY_MISSING"`) ||
		!strings.Contains(recorder.Body.String(), `"title":"Visible candidate"`) ||
		!strings.Contains(recorder.Body.String(), `"errorCode":"ASSET_HTTP_STATUS"`) ||
		!strings.Contains(recorder.Body.String(), `"name":"blocked.zip"`) ||
		!strings.Contains(recorder.Body.String(), `"archive":true`) ||
		!strings.Contains(recorder.Body.String(), `"archiveEntries":[{"crc32":"`+strings.Repeat("e", 8)+`","name":"blocked.gba","sizeBytes":4096}]`) ||
		!strings.Contains(recorder.Body.String(), `"scrapeRuns":[{"attemptCount":0,"candidateCount":1,"completedAtMs":`) ||
		!strings.Contains(recorder.Body.String(), `"provider":"HASHEOUS"`) {
		t.Fatalf("blocked review detail = %d %s", recorder.Code, recorder.Body.String())
	}
	uploadedCover := httptest.NewRecorder()
	invalidCoverRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reviews/"+itemID+"/assets", strings.NewReader(`{"uploadFileId":"`+coverUploadFileID+`","kind":"BACKGROUND"}`))
	invalidCoverRequest.SetPathValue("importItemId", itemID)
	invalidCoverRequest.Header.Set("Content-Type", "application/json")
	invalidCoverRequest.Header.Set("If-Match", `"v1"`)
	invalidCoverRequest.Header.Set("Idempotency-Key", uuid.NewString())
	server.createReviewAsset(uploadedCover, invalidCoverRequest)
	if uploadedCover.Code != http.StatusBadRequest || !strings.Contains(uploadedCover.Body.String(), `"code":"INVALID_REQUEST"`) {
		t.Fatalf("reject non-cover review asset = %d %s", uploadedCover.Code, uploadedCover.Body.String())
	}
	uploadedCover = httptest.NewRecorder()
	uploadedCoverRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reviews/"+itemID+"/assets", strings.NewReader(`{"uploadFileId":"`+coverUploadFileID+`","kind":"COVER"}`))
	uploadedCoverRequest.SetPathValue("importItemId", itemID)
	uploadedCoverRequest.Header.Set("Content-Type", "application/json")
	uploadedCoverRequest.Header.Set("If-Match", `"v1"`)
	uploadedCoverRequest.Header.Set("Idempotency-Key", uuid.NewString())
	server.createReviewAsset(uploadedCover, uploadedCoverRequest)
	if uploadedCover.Code != http.StatusCreated || !strings.Contains(uploadedCover.Body.String(), `"url":"/api/v1/admin/review-assets/`) {
		t.Fatalf("create review cover = %d %s", uploadedCover.Code, uploadedCover.Body.String())
	}
	var uploadedCoverResult struct {
		AssetID string `json:"assetId"`
	}
	if err := json.Unmarshal(uploadedCover.Body.Bytes(), &uploadedCoverResult); err != nil {
		t.Fatal(err)
	}
	patch := httptest.NewRecorder()
	patchRequest := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/reviews/"+itemID, strings.NewReader(`{"selectedAssets":{"coverCandidateAssetId":null,"coverUploadedAssetId":"`+uploadedCoverResult.AssetID+`","backgroundCandidateAssetId":null,"screenshotCandidateAssetIds":[]}}`))
	patchRequest.SetPathValue("importItemId", itemID)
	patchRequest.Header.Set("Content-Type", "application/json")
	patchRequest.Header.Set("If-Match", `"v1"`)
	server.patchReview(patch, patchRequest)
	if patch.Code != http.StatusOK || !strings.Contains(patch.Body.String(), `"version":2`) {
		t.Fatalf("select review cover = %d %s", patch.Code, patch.Body.String())
	}
	staleCover := httptest.NewRecorder()
	staleCoverRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reviews/"+itemID+"/assets", strings.NewReader(`{"uploadFileId":"`+coverUploadFileID+`","kind":"COVER"}`))
	staleCoverRequest.SetPathValue("importItemId", itemID)
	staleCoverRequest.Header.Set("Content-Type", "application/json")
	staleCoverRequest.Header.Set("If-Match", `"v1"`)
	staleCoverRequest.Header.Set("Idempotency-Key", uuid.NewString())
	server.createReviewAsset(staleCover, staleCoverRequest)
	if staleCover.Code != http.StatusConflict || !strings.Contains(staleCover.Body.String(), `"code":"REVIEW_VERSION_CONFLICT"`) {
		t.Fatalf("stale review cover upload = %d %s", staleCover.Code, staleCover.Body.String())
	}
	if _, err := server.database.Exec(`
UPDATE review_drafts SET cover_candidate_asset_id=? WHERE import_item_id=?
`, readyCoverAssetID, itemID); err == nil || !strings.Contains(err.Error(), "invalid review uploaded cover") {
		t.Fatalf("manual and candidate cover database invariant error = %v", err)
	}
	list := httptest.NewRecorder()
	server.reviews(list, httptest.NewRequest(http.MethodGet, "/api/v1/admin/reviews", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"sourceTotalSizeBytes":4096`) ||
		!strings.Contains(list.Body.String(), `"sourceMd5":"`+strings.Repeat("c", 32)+`"`) ||
		!strings.Contains(list.Body.String(), `"coverUrl":"/api/v1/admin/review-assets/`+uploadedCoverResult.AssetID+`"`) {
		t.Fatalf("review queue source projection = %d %s", list.Code, list.Body.String())
	}
	if _, err := server.database.Exec(`
UPDATE import_items SET state='PUBLISHED' WHERE id=?;
INSERT INTO review_events(id,import_item_id,event_type,actor,before_json,after_json,diff_json,
config_evidence_json,dat_evidence_json,provider_evidence_json,reason,created_at_ms)
VALUES('01980000-0000-7000-8000-000000000135',?,'APPROVED','local',?,
'{"schemaVersion":1,"decision":"APPROVED"}','{}','{}','{}','{}',NULL,?)
`, itemID, itemID, `{"schemaVersion":1,"selectedAssets":{"coverCandidateAssetId":"`+readyCoverAssetID+`"}}`, timestamp); err != nil {
		t.Fatal(err)
	}
	historicalCover := httptest.NewRecorder()
	historicalRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/review-assets/"+readyCoverAssetID, nil)
	historicalRequest.SetPathValue("assetId", readyCoverAssetID)
	server.reviewCandidateAsset(historicalCover, historicalRequest)
	if historicalCover.Code != http.StatusOK || !bytes.Equal(historicalCover.Body.Bytes(), coverPayload) {
		t.Fatalf("historical review cover = %d %q", historicalCover.Code, historicalCover.Body.Bytes())
	}
	historicalUploadedCover := httptest.NewRecorder()
	historicalUploadedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/review-assets/"+uploadedCoverResult.AssetID, nil)
	historicalUploadedRequest.SetPathValue("assetId", uploadedCoverResult.AssetID)
	server.reviewCandidateAsset(historicalUploadedCover, historicalUploadedRequest)
	if historicalUploadedCover.Code != http.StatusOK || !bytes.Equal(historicalUploadedCover.Body.Bytes(), coverPayload) {
		t.Fatalf("historical uploaded review cover = %d %q", historicalUploadedCover.Code, historicalUploadedCover.Body.Bytes())
	}
}

func TestGameDetailReturnsCoreValidationChoicesAndDOSPrograms(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	gameID := "01980000-0000-7000-8000-000000000101"
	metadataID := "01980000-0000-7000-8000-000000000102"
	contentID := "01980000-0000-7000-8000-000000000103"
	coverBlobID := "01980000-0000-7000-8000-000000000104"
	coverAssetID := "01980000-0000-7000-8000-000000000105"
	variantID := "01980000-0000-7000-8000-000000000106"
	variantRevisionID := "01980000-0000-7000-8000-000000000107"
	saveStateID := "01980000-0000-7000-8000-000000000108"
	coreArtifactID := "01980000-0000-7000-8000-000000000109"
	transaction, err := server.database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.Exec(`
PRAGMA defer_foreign_keys=ON
`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	if _, err := transaction.Exec(`
INSERT INTO game_metadata_revisions(id,
game_id,
title,
description,
developer,
publisher,
genre,
players,
release_year,
source_kind,
source_ref_id,
created_at_ms) VALUES(?,
?,
'Doom',
'',
'',
'',
'',
1,
1993,
'IMPORT_REVIEW',
'review',
?)
`, metadataID, gameID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO game_content_revisions(id,
game_id,
source_kind,
source_ref_id,
source_manifest_json,
source_manifest_digest,
created_at_ms) VALUES(?,
?,
'IMPORT_REVIEW',
'review',
'{}',
?,
?)
`, contentID, gameID, strings.Repeat("0", 64), now); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO games(id,
platform_instance_id,
status,
current_metadata_revision_id,
current_content_revision_id,
search_text,
version,
created_at_ms,
updated_at_ms) VALUES(?,
'01980000-0000-7000-8000-000000000009',
'PUBLISHED',
?,
?,
'doom',
1,
?,
?)
`, gameID, metadataID, contentID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO blobs(id,
sha256,
size_bytes,
md5,
sha1,
crc32,
media_type,
created_at_ms) VALUES(?,
?,
4,
?,
?,
?,
'image/png',
?)
`, coverBlobID, strings.Repeat("1", 64), strings.Repeat("2", 32), strings.Repeat("3", 40),
		strings.Repeat("4", 8), now); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO game_assets(id,
game_id,
metadata_revision_id,
blob_id,
kind,
ordinal,
width_px,
height_px,
media_type,
created_at_ms) VALUES(?,
?,
?,
?,
'COVER',
0,
600,
800,
'image/png',
?)
`, coverAssetID, gameID, metadataID, coverBlobID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO dos_entries(game_content_revision_id,
normalized_path,
original_relative_path,
kind,
rank,
enabled,
direct_launch_safe) VALUES(?,
 'GAMES/DOOM.EXE',
'GAMES/DOOM.EXE',
'EXE',
0,
1,
1),
(?,
 'SETUP%.BAT',
'SETUP%.BAT',
'BAT',
1,
1,
0)
`, contentID, contentID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO core_artifacts(id,core_id,emulatorjs_version,bundle_version,flavor,relative_path,size_bytes,sha256,
source_commit,provenance_json,compatibility_config_json,enabled,version,created_at_ms,updated_at_ms)
VALUES(?,'dosbox_pure','4.2.3','test','THREAD_WASM','cores/dosbox_pure.js',1,?,NULL,'{}','{}',1,1,?,?)
`, coreArtifactID, strings.Repeat("6", 64), now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO game_variants(id,game_id,core_id,current_revision_id,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,NULL,1,?,?)
`, variantID, gameID, "dosbox_pure", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO game_variant_revisions(id,game_variant_id,game_content_revision_id,core_artifact_id,dat_version_id,
validation_input_digest,emulator_game_id,status,compatibility_code,dependency_snapshot_json,default_dos_entry,created_at_ms)
VALUES(?,?,?,?,NULL,?,9001,'READY','READY','{}',NULL,?)
`, variantRevisionID, variantID, contentID, coreArtifactID, strings.Repeat("5", 64), now); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec("UPDATE game_variants SET current_revision_id=? WHERE id=?", variantRevisionID, variantID); err != nil {
		t.Fatal(err)
	}
	screenshot := []byte("retrom-save-screenshot")
	screenshotMetadata, err := server.blobs.Put(bytes.NewReader(screenshot))
	if err != nil {
		t.Fatal(err)
	}
	screenshotBlobID, err := blobstore.EnsureRecord(t.Context(), transaction, screenshotMetadata, "image/png", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO save_states(id,profile_id,game_id,game_variant_revision_id,core_artifact_id,dat_version_id,dos_entry_path,
state_blob_id,screenshot_blob_id,name,active_duration_ms,version,created_at_ms,updated_at_ms,deleted_at_ms)
VALUES(?,'local',?,?,?,NULL,NULL,?,?,'入口存档',180000,1,?,?,NULL)
`, saveStateID, gameID, variantRevisionID, coreArtifactID, screenshotBlobID, screenshotBlobID, now, now); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 8; index++ {
		if _, err := transaction.Exec(`
INSERT INTO save_states(id,profile_id,game_id,game_variant_revision_id,core_artifact_id,dat_version_id,dos_entry_path,
state_blob_id,screenshot_blob_id,name,active_duration_ms,version,created_at_ms,updated_at_ms,deleted_at_ms)
VALUES(?,'local',?,?,?,NULL,NULL,?,?,?,60000,1,?,?,NULL)
`, uuid.NewString(), gameID, variantRevisionID, coreArtifactID, screenshotBlobID, screenshotBlobID,
			fmt.Sprintf("额外存档 %d", index+1), now+int64(index+1), now+int64(index+1)); err != nil {
			t.Fatal(err)
		}
	}
	var latestLaunchID string
	for index, duration := range []int64{120_000, 240_000} {
		launchID, playID := uuid.NewString(), uuid.NewString()
		latestLaunchID = launchID
		if _, err := transaction.Exec(`
INSERT INTO launch_sessions(id,profile_id,game_id,game_variant_revision_id,core_artifact_id,return_to,
credential_sha256,state,bootstrap_expires_at_ms,finished_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms,version)
VALUES(?,'local',?,?,?,'/',zeroblob(32),'FINISHED',?,?,?, ?,?,1)
`, launchID, gameID, variantRevisionID, coreArtifactID, now+60_000, now+int64(index), now+120_000, now, now+int64(index)); err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.Exec(`
INSERT INTO play_sessions(id,launch_session_id,profile_id,game_id,game_variant_revision_id,started_at_ms,
last_heartbeat_at_ms,ended_at_ms,active_duration_ms,last_client_sequence,state,version,created_at_ms,updated_at_ms)
VALUES(?,?,'local',?,?, ?,?,?,?,1,'FINISHED',1,?,?)
`, playID, launchID, gameID, variantRevisionID, now-20_000+int64(index)*10_000, now, now, duration,
			now, now+int64(10-index)); err != nil {
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/games/"+gameID, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("game detail status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		DefaultDOSEntry *string `json:"defaultDosEntry"`
		CoverURL        *string `json:"coverUrl"`
		CoreOptions     []struct {
			CoreID string `json:"coreId"`
			Status string `json:"status"`
		} `json:"coreOptions"`
		DOSEntries []struct {
			Path             string `json:"path"`
			DirectLaunchSafe bool   `json:"directLaunchSafe"`
		} `json:"dosEntries"`
		SaveStates []struct {
			SaveStateID   string `json:"saveStateId"`
			ScreenshotURL string `json:"screenshotUrl"`
		} `json:"saveStates"`
		SaveStateCount int64 `json:"saveStateCount"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.CoreOptions) != 1 || response.CoreOptions[0].CoreID != "dosbox_pure" ||
		response.CoreOptions[0].Status != "READY" {
		t.Fatalf("core options = %#v", response.CoreOptions)
	}
	expectedCoverURL := "/content/assets/" + coverAssetID
	if response.CoverURL == nil || *response.CoverURL != expectedCoverURL || response.DefaultDOSEntry != nil ||
		len(response.DOSEntries) != 2 ||
		response.DOSEntries[0].Path != "GAMES/DOOM.EXE" ||
		!response.DOSEntries[0].DirectLaunchSafe ||
		response.DOSEntries[1].DirectLaunchSafe || len(response.SaveStates) != 8 || response.SaveStateCount != 9 {
		t.Fatalf("DOS choices = default:%v entries:%#v", response.DefaultDOSEntry, response.DOSEntries)
	}
	list := httptest.NewRecorder()
	server.Handler().ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/games?limit=100", nil))
	if list.Code != http.StatusOK ||
		!strings.Contains(list.Body.String(), `"coverUrl":"`+expectedCoverURL+`"`) ||
		!strings.Contains(list.Body.String(), `"defaultCore":{"id":"dosbox_pure","name":"DOSBox Pure"}`) ||
		!strings.Contains(list.Body.String(), `"lastPlayedAtMs":`) ||
		!strings.Contains(list.Body.String(), `"createdAtMs":`) ||
		!strings.Contains(list.Body.String(), `"generatedAtMs":`) {
		t.Fatalf("game list cover = %d: %s", list.Code, list.Body.String())
	}
	adminList := httptest.NewRecorder()
	server.Handler().ServeHTTP(adminList, httptest.NewRequest(http.MethodGet, "/api/v1/admin/games?limit=100", nil))
	if adminList.Code != http.StatusOK ||
		!strings.Contains(adminList.Body.String(), `"releaseYear":`) ||
		!strings.Contains(adminList.Body.String(), `"metadataComplete":`) ||
		!strings.Contains(adminList.Body.String(), `"runtimeStatus":"READY"`) {
		t.Fatalf("admin game health projection = %d: %s", adminList.Code, adminList.Body.String())
	}
	adminDetail := httptest.NewRecorder()
	server.Handler().ServeHTTP(adminDetail, httptest.NewRequest(http.MethodGet, "/api/v1/admin/games/"+gameID, nil))
	if adminDetail.Code != http.StatusOK || !strings.Contains(adminDetail.Body.String(), `"generatedAtMs":`) {
		t.Fatalf("admin game detail generated time = %d: %s", adminDetail.Code, adminDetail.Body.String())
	}
	saves := httptest.NewRecorder()
	server.Handler().ServeHTTP(saves, httptest.NewRequest(http.MethodGet, "/api/v1/saves", nil))
	if saves.Code != http.StatusOK ||
		!strings.Contains(saves.Body.String(), `"screenshotUrl":"`+saveStateScreenshotURL(saveStateID)+`"`) ||
		!strings.Contains(saves.Body.String(), `"activeDurationMs":180000`) ||
		!strings.Contains(saves.Body.String(), `"platform":{"id":"dos","name":"MS-DOS"}`) ||
		!strings.Contains(saves.Body.String(), `"generatedAtMs":`) {
		t.Fatalf("save list projection = %d: %s", saves.Code, saves.Body.String())
	}
	filteredSaves := httptest.NewRecorder()
	server.Handler().ServeHTTP(filteredSaves, httptest.NewRequest(http.MethodGet, "/api/v1/saves?gameId="+gameID, nil))
	if filteredSaves.Code != http.StatusOK || !strings.Contains(filteredSaves.Body.String(), `"gameId":"`+gameID+`"`) {
		t.Fatalf("save game filter = %d: %s", filteredSaves.Code, filteredSaves.Body.String())
	}
	missingGameSaves := httptest.NewRecorder()
	server.Handler().ServeHTTP(missingGameSaves, httptest.NewRequest(http.MethodGet, "/api/v1/saves?gameId="+uuid.NewString(), nil))
	if missingGameSaves.Code != http.StatusOK || !strings.Contains(missingGameSaves.Body.String(), `"items":[]`) {
		t.Fatalf("save missing game filter = %d: %s", missingGameSaves.Code, missingGameSaves.Body.String())
	}
	home := httptest.NewRecorder()
	server.Handler().ServeHTTP(home, httptest.NewRequest(http.MethodGet, "/api/v1/home", nil))
	if home.Code != http.StatusOK || !strings.Contains(home.Body.String(), `"recentSaves":[{"activeDurationMs":60000`) {
		t.Fatalf("home recent save duration = %d: %s", home.Code, home.Body.String())
	}
	var homeResponse struct {
		FeaturedGame *struct {
			GameID          string `json:"gameId"`
			HasSaveStates   bool   `json:"hasSaveStates"`
			LastSessionSave *struct {
				SaveStateID string `json:"saveStateId"`
			} `json:"lastSessionSave"`
		} `json:"featuredGame"`
		RecentGames []struct {
			GameID       string `json:"gameId"`
			SessionCount int64  `json:"sessionCount"`
		} `json:"recentGames"`
		LatestGames []struct {
			GameID      string `json:"gameId"`
			Title       string `json:"title"`
			CreatedAtMS int64  `json:"createdAtMs"`
		} `json:"latestGames"`
		QuickPlatforms []homePlatform `json:"quickPlatforms"`
	}
	if err := json.Unmarshal(home.Body.Bytes(), &homeResponse); err != nil {
		t.Fatal(err)
	}
	if homeResponse.FeaturedGame == nil || homeResponse.FeaturedGame.GameID != gameID ||
		!homeResponse.FeaturedGame.HasSaveStates || homeResponse.FeaturedGame.LastSessionSave != nil ||
		len(homeResponse.RecentGames) != 1 || homeResponse.RecentGames[0].SessionCount != 2 ||
		len(homeResponse.LatestGames) != 1 || homeResponse.LatestGames[0].GameID != gameID ||
		homeResponse.LatestGames[0].CreatedAtMS != now ||
		len(homeResponse.QuickPlatforms) != 4 || homeResponse.QuickPlatforms[0].ID != "dos" ||
		homeResponse.QuickPlatforms[0].PlayCount != 2 {
		t.Fatalf("home projection = %#v", homeResponse)
	}
	sessionSaveID := uuid.NewString()
	if _, err := server.database.Exec(`
INSERT INTO save_states(id,profile_id,game_id,game_variant_revision_id,core_artifact_id,dat_version_id,dos_entry_path,
state_blob_id,screenshot_blob_id,source_launch_session_id,name,active_duration_ms,version,created_at_ms,updated_at_ms,deleted_at_ms)
VALUES(?,'local',?,?,?,NULL,NULL,?,?,?,'本次游玩存档',240000,1,?,?,NULL)
`, sessionSaveID, gameID, variantRevisionID, coreArtifactID, screenshotBlobID, screenshotBlobID,
		latestLaunchID, now+20, now+20); err != nil {
		t.Fatal(err)
	}
	if _, err := server.database.Exec(`UPDATE save_states SET source_launch_session_id=NULL WHERE id=?`, sessionSaveID); err == nil ||
		!strings.Contains(err.Error(), "source launch is immutable") {
		t.Fatalf("mutable save source error = %v", err)
	}
	homeWithSessionSave := httptest.NewRecorder()
	server.Handler().ServeHTTP(homeWithSessionSave, httptest.NewRequest(http.MethodGet, "/api/v1/home", nil))
	if homeWithSessionSave.Code != http.StatusOK {
		t.Fatalf("home with session save = %d: %s", homeWithSessionSave.Code, homeWithSessionSave.Body.String())
	}
	if err := json.Unmarshal(homeWithSessionSave.Body.Bytes(), &homeResponse); err != nil {
		t.Fatal(err)
	}
	if homeResponse.FeaturedGame == nil || homeResponse.FeaturedGame.LastSessionSave == nil ||
		homeResponse.FeaturedGame.LastSessionSave.SaveStateID != sessionSaveID {
		t.Fatalf("featured session save = %#v", homeResponse.FeaturedGame)
	}
	seedRecentGameHistory(t, server.database, coreArtifactID, now, 55)
	latest := httptest.NewRecorder()
	server.Handler().ServeHTTP(latest, httptest.NewRequest(http.MethodGet, "/api/v1/home", nil))
	if latest.Code != http.StatusOK {
		t.Fatalf("home latest games = %d: %s", latest.Code, latest.Body.String())
	}
	var latestResponse struct {
		LatestGames []struct {
			Title       string `json:"title"`
			CreatedAtMS int64  `json:"createdAtMs"`
		} `json:"latestGames"`
	}
	if err := json.Unmarshal(latest.Body.Bytes(), &latestResponse); err != nil {
		t.Fatal(err)
	}
	if len(latestResponse.LatestGames) != 10 || latestResponse.LatestGames[0].Title != "Recent fixture 54" ||
		latestResponse.LatestGames[9].Title != "Recent fixture 45" {
		t.Fatalf("latest game order = %#v", latestResponse.LatestGames)
	}
	for index := 1; index < len(latestResponse.LatestGames); index++ {
		if latestResponse.LatestGames[index-1].CreatedAtMS <= latestResponse.LatestGames[index].CreatedAtMS {
			t.Fatalf("latest game timestamps are not descending: %#v", latestResponse.LatestGames)
		}
	}
	recent := httptest.NewRecorder()
	server.Handler().ServeHTTP(recent, httptest.NewRequest(http.MethodGet, "/api/v1/recent-games", nil))
	if recent.Code != http.StatusOK || !strings.Contains(recent.Body.String(), `"activeDurationMs":360000`) ||
		!strings.Contains(recent.Body.String(), `"sessionCount":2`) || strings.Contains(recent.Body.String(), `"limit"`) ||
		!strings.Contains(recent.Body.String(), `"coverUrl":"`+expectedCoverURL+`"`) ||
		!strings.Contains(recent.Body.String(), `"generatedAtMs":`) {
		t.Fatalf("recent games projection = %d: %s", recent.Code, recent.Body.String())
	}
	var recentResponse struct {
		Items []recentGameProjection `json:"items"`
	}
	if err := json.Unmarshal(recent.Body.Bytes(), &recentResponse); err != nil || len(recentResponse.Items) != 56 {
		t.Fatalf("unbounded recent games count = %d, error = %v", len(recentResponse.Items), err)
	}
	invalidRecentLimit := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalidRecentLimit, httptest.NewRequest(http.MethodGet, "/api/v1/recent-games?limit=50", nil))
	if invalidRecentLimit.Code != http.StatusBadRequest || !strings.Contains(invalidRecentLimit.Body.String(), `"code":"INVALID_QUERY"`) {
		t.Fatalf("recent games invalid limit = %d: %s", invalidRecentLimit.Code, invalidRecentLimit.Body.String())
	}
	screenshotResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(
		screenshotResponse,
		httptest.NewRequest(http.MethodGet, saveStateScreenshotURL(saveStateID), nil),
	)
	if screenshotResponse.Code != http.StatusOK || screenshotResponse.Body.String() != string(screenshot) ||
		screenshotResponse.Header().Get("Cache-Control") != "private, no-store" ||
		screenshotResponse.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("save screenshot = %d headers=%v body=%q", screenshotResponse.Code, screenshotResponse.Header(), screenshotResponse.Body.String())
	}
	if _, err := server.database.Exec("UPDATE save_states SET deleted_at_ms=? WHERE id=?", now+1, saveStateID); err != nil {
		t.Fatal(err)
	}
	deletedScreenshot := httptest.NewRecorder()
	server.Handler().ServeHTTP(
		deletedScreenshot,
		httptest.NewRequest(http.MethodGet, saveStateScreenshotURL(saveStateID), nil),
	)
	if deletedScreenshot.Code != http.StatusNotFound ||
		!strings.Contains(deletedScreenshot.Body.String(), `"code":"SAVE_SCREENSHOT_NOT_FOUND"`) {
		t.Fatalf("deleted save screenshot = %d: %s", deletedScreenshot.Code, deletedScreenshot.Body.String())
	}
}

func seedRecentGameHistory(t *testing.T, database *sql.DB, coreArtifactID string, now int64, count int) {
	t.Helper()
	transaction, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.Exec("PRAGMA defer_foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < count; index++ {
		gameID := uuid.NewString()
		metadataID := uuid.NewString()
		contentID := uuid.NewString()
		variantID := uuid.NewString()
		variantRevisionID := uuid.NewString()
		launchID := uuid.NewString()
		playID := uuid.NewString()
		if _, err := transaction.Exec(`
INSERT INTO game_metadata_revisions(id,game_id,title,description,developer,publisher,genre,players,release_year,
source_kind,source_ref_id,created_at_ms)
VALUES(?,?,?,'','','','',NULL,NULL,'ADMIN_EDIT',NULL,?)
`, metadataID, gameID, fmt.Sprintf("Recent fixture %02d", index), now); err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.Exec(`
INSERT INTO game_content_revisions(id,game_id,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms)
VALUES(?,?,'ADMIN_REPLACE',?,'{}',?,?)
`, contentID, gameID, fmt.Sprintf("recent-%d", index), strings.Repeat("7", 64), now); err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.Exec(`
INSERT INTO games(id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,search_text,
version,created_at_ms,updated_at_ms)
VALUES(?,'01980000-0000-7000-8000-000000000009','PUBLISHED',?,?,?,1,?,?)
`, gameID, metadataID, contentID, fmt.Sprintf("recent fixture %02d", index), now+int64(index), now+int64(index)); err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.Exec(`
INSERT INTO game_variants(id,game_id,core_id,current_revision_id,version,created_at_ms,updated_at_ms)
VALUES(?,?,'dosbox_pure',NULL,1,?,?)
`, variantID, gameID, now, now); err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.Exec(`
INSERT INTO game_variant_revisions(id,game_variant_id,game_content_revision_id,core_artifact_id,dat_version_id,
validation_input_digest,emulator_game_id,status,compatibility_code,dependency_snapshot_json,default_dos_entry,created_at_ms)
VALUES(?,?,?,?,NULL,?,?,'READY','READY','{}',NULL,?)
`, variantRevisionID, variantID, contentID, coreArtifactID, strings.Repeat("8", 64), 10_000+index, now); err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.Exec("UPDATE game_variants SET current_revision_id=? WHERE id=?", variantRevisionID, variantID); err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.Exec(`
INSERT INTO launch_sessions(id,profile_id,game_id,game_variant_revision_id,core_artifact_id,return_to,credential_sha256,
state,bootstrap_expires_at_ms,finished_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms,version)
VALUES(?,'local',?,?,?,'/recent',zeroblob(32),'FINISHED',?,?,?,?,?,1)
`, launchID, gameID, variantRevisionID, coreArtifactID, now+60_000, now, now+120_000, now, now); err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.Exec(`
INSERT INTO play_sessions(id,launch_session_id,profile_id,game_id,game_variant_revision_id,started_at_ms,
last_heartbeat_at_ms,ended_at_ms,active_duration_ms,last_client_sequence,state,version,created_at_ms,updated_at_ms)
VALUES(?,?,'local',?,?,?,?,?,60000,1,'FINISHED',1,?,?)
`, playID, launchID, gameID, variantRevisionID, now-int64(index+1)*1_000, now, now, now, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAPIValidationAllowsNestedRuntimePath(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	recorder := httptest.NewRecorder()
	server.Handler().
		ServeHTTP(recorder, httptest.NewRequest(http.MethodHead, "/runtime/emulatorjs/4.2.3/data/loader.js", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("nested runtime path status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestRuntimeDOSConfigRouteRequiresLaunchCredential(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(
		recorder,
		httptest.NewRequest(
			http.MethodGet,
			"/runtime/launches/01980000-0000-7000-8000-000000000001/dos-config/game.conf",
			nil,
		),
	)
	if recorder.Code != http.StatusUnauthorized ||
		!strings.Contains(recorder.Body.String(), `"code":"LAUNCH_CREDENTIAL_INVALID"`) {
		t.Fatalf("DOS config without credential = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestOpenAPIValidationRejectsUnknownJSONAndMapsMissingPrecondition(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	handler := server.Handler()
	sessionRecorder := httptest.NewRecorder()
	handler.ServeHTTP(sessionRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/session", nil))
	var sessionBody struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(sessionRecorder.Body.Bytes(), &sessionBody); err != nil {
		t.Fatal(err)
	}
	cookie := sessionRecorder.Result().Cookies()[0]
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/launches",
		strings.NewReader(
			`{"gameId":"01980000-0000-7000-8000-000000000001","coreId":null,"saveStateId":null,"dosEntry":null,"returnTo":"/","clientCapabilities":{"secureContext":true,"crossOriginIsolated":true,"sharedArrayBuffer":true},"unknown":true}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "01980000-0000-7000-8000-000000000099")
	setCSRFCredentials(request, cookie, sessionBody.CSRFToken)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"INVALID_REQUEST"`) {
		t.Fatalf("unknown JSON response = %d %s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/saves/01980000-0000-7000-8000-000000000001",
		strings.NewReader(`{"name":"slot"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	setCSRFCredentials(request, cookie, sessionBody.CSRFToken)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusPreconditionRequired ||
		!strings.Contains(recorder.Body.String(), `"code":"PRECONDITION_REQUIRED"`) {
		t.Fatalf("missing If-Match response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestOpenAPIHasExactlyThreeStreamingOperations(t *testing.T) {
	t.Parallel()
	specification, err := generated.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	operationIDs := make([]string, 0, 3)
	for _, pathItem := range specification.Paths.Map() {
		for _, operation := range pathItem.Operations() {
			if enabled, ok := operation.Extensions["x-retrom-streaming-body"].(bool); ok && enabled {
				operationIDs = append(operationIDs, operation.OperationID)
			}
		}
	}
	slices.Sort(operationIDs)
	wanted := []string{"PostRuntimeSaveState", "PutAdminUploadPart", "PutRuntimePersistentSave"}
	if !slices.Equal(operationIDs, wanted) {
		t.Fatalf("streaming operations = %v", operationIDs)
	}
}

func TestGenericIdempotencySerializesConcurrentCreates(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	handler := server.Handler()
	sessionRecorder := httptest.NewRecorder()
	handler.ServeHTTP(sessionRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/session", nil))
	var sessionBody struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(sessionRecorder.Body.Bytes(), &sessionBody); err != nil {
		t.Fatal(err)
	}
	cookie := sessionRecorder.Result().Cookies()[0]
	key := "01980000-0000-7000-8000-000000000077"
	body := `{"platformId":"nes","defaultCoreId":"fceumm","name":"Concurrent Directory","description":"","sortOrder":99}`
	send := func(contents string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/platform-instances", strings.NewReader(contents))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", key)
		setCSRFCredentials(request, cookie, sessionBody.CSRFToken)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	responses := make([]*httptest.ResponseRecorder, 2)
	var wait sync.WaitGroup
	for index := range responses {
		wait.Add(1)
		go func() {
			defer wait.Done()
			responses[index] = send(body)
		}()
	}
	wait.Wait()
	if responses[0].Code != http.StatusCreated || responses[1].Code != http.StatusCreated ||
		responses[0].Body.String() != responses[1].Body.String() {
		t.Fatalf(
			"idempotent responses = %d/%d %q/%q",
			responses[0].Code,
			responses[1].Code,
			responses[0].Body.String(),
			responses[1].Body.String(),
		)
	}
	var count int
	if err := server.database.QueryRow(`
SELECT count(*)
FROM platform_instances
WHERE slug='concurrent-directory'
`).Scan(&count); err != nil ||
		count != 1 {
		t.Fatalf("created rows = %d, error=%v", count, err)
	}
	conflict := send(
		`{"platformId":"nes","defaultCoreId":"fceumm","name":"Different Directory","description":"","sortOrder":99}`,
	)
	if conflict.Code != http.StatusConflict ||
		!strings.Contains(conflict.Body.String(), `"code":"IDEMPOTENCY_KEY_REUSED"`) {
		t.Fatalf("idempotency conflict = %d %s", conflict.Code, conflict.Body.String())
	}
}

func TestPlatformLifecycleUsesImpactDigestVersioningAndAudit(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	if _, err := server.database.Exec(`
INSERT INTO core_artifacts(id,
core_id,
emulatorjs_version,
bundle_version,
flavor,
relative_path,
size_bytes,
sha256,
source_commit,
provenance_json,
compatibility_config_json,
enabled,
version,
created_at_ms,
updated_at_ms) VALUES('01980000-0000-7000-8000-000000000099',
'mgba',
'4.2.3',
'test',
'WASM',
'data/cores/mgba-test.data',
1,
?,
NULL,
'{}',
'{}',
1,
1,
0,
0)
`, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	sessionRecorder := httptest.NewRecorder()
	handler.ServeHTTP(sessionRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/session", nil))
	var sessionBody struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(sessionRecorder.Body.Bytes(), &sessionBody); err != nil {
		t.Fatal(err)
	}
	cookie := sessionRecorder.Result().Cookies()[0]
	send := func(method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		if body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		for name, value := range headers {
			request.Header.Set(name, value)
		}
		setCSRFCredentials(request, cookie, sessionBody.CSRFToken)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	created := send(
		http.MethodPost,
		"/api/v1/admin/platform-instances",
		`{"platformId":"gbc","defaultCoreId":"gambatte","name":"Handheld Zone","description":"测试目录","sortOrder":120}`,
		map[string]string{"Idempotency-Key": uuid.NewString()},
	)
	if created.Code != http.StatusCreated {
		t.Fatalf("create platform instance = %d %s", created.Code, created.Body.String())
	}
	var createdBody struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	invalidCore := send(
		http.MethodPost,
		"/api/v1/admin/platform-instances",
		`{"platformId":"gbc","defaultCoreId":"fceumm","name":"错误核心","description":"","sortOrder":122}`,
		map[string]string{"Idempotency-Key": uuid.NewString()},
	)
	if invalidCore.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(invalidCore.Body.String(), `"code":"PLATFORM_DEFAULT_CORE_INVALID"`) {
		t.Fatalf("invalid platform core = %d %s", invalidCore.Code, invalidCore.Body.String())
	}
	duplicateName := send(
		http.MethodPost,
		"/api/v1/admin/platform-instances",
		`{"platformId":"gbc","defaultCoreId":"gambatte","name":"Handheld Zone","description":"","sortOrder":123}`,
		map[string]string{"Idempotency-Key": uuid.NewString()},
	)
	if duplicateName.Code != http.StatusCreated || !strings.Contains(duplicateName.Body.String(), `"slug":"handheld-zone-2"`) {
		t.Fatalf("generated duplicate platform slug = %d %s", duplicateName.Code, duplicateName.Body.String())
	}
	patched := send(
		http.MethodPatch,
		"/api/v1/admin/platform-instances/"+createdBody.ID,
		`{"name":"掌机典藏","description":"测试目录","sortOrder":121,"enabled":true}`,
		map[string]string{"If-Match": `"v1"`},
	)
	if patched.Code != http.StatusOK || patched.Header().Get("ETag") != `"v2"` {
		t.Fatalf("patch platform instance = %d %s", patched.Code, patched.Body.String())
	}
	preview := send(
		http.MethodPost,
		"/api/v1/admin/platform-instances/"+createdBody.ID+"/default-core-preview",
		`{"coreId":"mgba","cursor":null,"limit":50}`,
		map[string]string{"If-Match": `"v2"`, "Idempotency-Key": uuid.NewString()},
	)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview default core = %d %s", preview.Code, preview.Body.String())
	}
	var previewBody struct {
		ImpactDigest string `json:"impactDigest"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &previewBody); err != nil || previewBody.ImpactDigest == "" {
		t.Fatalf("preview body = %s, error=%v", preview.Body.String(), err)
	}
	changed := send(
		http.MethodPost,
		"/api/v1/admin/platform-instances/"+createdBody.ID+"/default-core",
		fmt.Sprintf(`{"coreId":"mgba","impactDigest":%q,"confirmBlocked":false}`, previewBody.ImpactDigest),
		map[string]string{"If-Match": `"v2"`, "Idempotency-Key": uuid.NewString()},
	)
	if changed.Code != http.StatusOK || changed.Header().Get("ETag") != `"v3"` {
		t.Fatalf("change default core = %d %s", changed.Code, changed.Body.String())
	}
	stale := send(
		http.MethodPost,
		"/api/v1/admin/platform-instances/"+createdBody.ID+"/default-core",
		fmt.Sprintf(`{"coreId":"gambatte","impactDigest":%q,"confirmBlocked":false}`, previewBody.ImpactDigest),
		map[string]string{"If-Match": `"v2"`, "Idempotency-Key": uuid.NewString()},
	)
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), `"code":"IMPACT_PREVIEW_STALE"`) {
		t.Fatalf("stale impact = %d %s", stale.Code, stale.Body.String())
	}
	deleted := send(
		http.MethodDelete,
		"/api/v1/admin/platform-instances/"+createdBody.ID,
		"",
		map[string]string{"If-Match": `"v3"`},
	)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete platform instance = %d %s", deleted.Code, deleted.Body.String())
	}
	reusedSlug := send(
		http.MethodPost,
		"/api/v1/admin/platform-instances",
		`{"platformId":"gbc","defaultCoreId":"gambatte","name":"Handheld Zone","description":"","sortOrder":124}`,
		map[string]string{"Idempotency-Key": uuid.NewString()},
	)
	if reusedSlug.Code != http.StatusCreated || !strings.Contains(reusedSlug.Body.String(), `"slug":"handheld-zone-3"`) {
		t.Fatalf("deleted platform slug was reused = %d %s", reusedSlug.Code, reusedSlug.Body.String())
	}
	var actions int
	if err := server.database.QueryRow(`
SELECT count(*)
FROM audit_events
WHERE resource_type='PLATFORM_INSTANCE'
AND resource_id=?
`, createdBody.ID).Scan(&actions); err != nil ||
		actions != 4 {
		t.Fatalf("platform audit actions = %d, error=%v", actions, err)
	}
}

func TestPlatformInstanceOrderIsAtomicVersionedAndExact(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	handler := server.Handler()
	create := func(name string, sortOrder int) string {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/platform-instances", strings.NewReader(fmt.Sprintf(
			`{"platformId":"gbc","defaultCoreId":"gambatte","name":%q,"description":"","sortOrder":%d}`,
			name, sortOrder,
		)))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", uuid.NewString())
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("create reorder fixture = %d %s", recorder.Code, recorder.Body.String())
		}
		var body struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.ID
	}
	firstID := create("第一目录", 100)
	secondID := create("第二目录", 200)

	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/admin/platform-instances", nil))
	if list.Code != http.StatusOK || strings.Count(list.Body.String(), `"gameCount":0`) != 25 {
		t.Fatalf("platform game counts = %d %s", list.Code, list.Body.String())
	}
	sendOrder := func(body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/platform-instances/order", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	type orderItem struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
	}
	items := []orderItem{{ID: secondID, Version: 1}, {ID: firstID, Version: 1}}
	func() {
		rows, err := server.database.Query(
			"SELECT id,version FROM platform_instances WHERE deleted_at_ms IS NULL ORDER BY sort_order,id",
		)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { cleanup.Error("close", rows.Close()) }()
		for rows.Next() {
			var item orderItem
			if err := rows.Scan(&item.ID, &item.Version); err != nil {
				t.Fatal(err)
			}
			if item.ID != firstID && item.ID != secondID {
				items = append(items, item)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	}()
	orderBody, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		t.Fatal(err)
	}
	reordered := sendOrder(string(orderBody))
	if reordered.Code != http.StatusOK || !strings.Contains(reordered.Body.String(), `"sortOrder":100`) ||
		!strings.Contains(reordered.Body.String(), `"version":2`) {
		t.Fatalf("platform reorder = %d %s", reordered.Code, reordered.Body.String())
	}
	var firstSort, firstVersion, secondSort, secondVersion int64
	if err := server.database.QueryRow("SELECT sort_order,version FROM platform_instances WHERE id=?", firstID).Scan(&firstSort, &firstVersion); err != nil {
		t.Fatal(err)
	}
	if err := server.database.QueryRow("SELECT sort_order,version FROM platform_instances WHERE id=?", secondID).Scan(&secondSort, &secondVersion); err != nil {
		t.Fatal(err)
	}
	if secondSort != 100 || firstSort != 200 || firstVersion != 2 || secondVersion != 2 {
		t.Fatalf("stored reorder first=%d/v%d second=%d/v%d", firstSort, firstVersion, secondSort, secondVersion)
	}
	stale := sendOrder(string(orderBody))
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), `"code":"VERSION_CONFLICT"`) {
		t.Fatalf("stale reorder = %d %s", stale.Code, stale.Body.String())
	}
	incomplete := sendOrder(fmt.Sprintf(`{"items":[{"id":%q,"version":2}]}`, firstID))
	if incomplete.Code != http.StatusConflict || !strings.Contains(incomplete.Body.String(), `"code":"PLATFORM_INSTANCE_ORDER_STALE"`) {
		t.Fatalf("incomplete reorder = %d %s", incomplete.Code, incomplete.Body.String())
	}
}

func TestJobEventStreamUsesTransactionalSnapshotAndGlobalCursor(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	now := time.Now().UnixMilli()
	targetID := "01980000-0000-7000-8000-000000000081"
	otherID := "01980000-0000-7000-8000-000000000082"
	for index, jobID := range []string{targetID, otherID} {
		dedupe := strings.Repeat(string(rune('a'+index)), 64)
		if _, err := server.database.Exec(`
INSERT INTO jobs(id,
scope_type,
scope_id,
kind,
dedupe_key,
execution_no,
payload_json,
cancellable,
state,
attempt_count,
max_attempts,
available_at_ms,
finished_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
'GAME_VARIANT',
?,
'VARIANT_REVALIDATE',
?,
1,
'{}',
0,
'SUCCEEDED',
1,
2,
?,
?,
?,
?)
`, jobID, jobID, dedupe, now, now, now, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := server.database.Exec(`
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES
(?,
'GAME_VARIANT',
?,
'STARTED',
'{"source":"target-old"}',
?),
(?,
'GAME_VARIANT',
?,
'SUCCEEDED',
'{"source":"other"}',
?)
`, targetID, targetID, now, otherID, otherID, now); err != nil {
		t.Fatal(err)
	}

	snapshot := httptest.NewRecorder()
	server.Handler().
		ServeHTTP(snapshot, httptest.NewRequest(http.MethodGet, "/api/v1/admin/jobs/"+targetID+"/events", nil))
	if snapshot.Code != http.StatusOK || !strings.Contains(snapshot.Body.String(), "id: 2\nevent: snapshot") ||
		!strings.Contains(snapshot.Body.String(), `"state":"SUCCEEDED"`) ||
		strings.Contains(snapshot.Body.String(), "target-old") {
		t.Fatalf("snapshot stream = %d %s", snapshot.Code, snapshot.Body.String())
	}
	if _, err := server.database.Exec(`
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'GAME_VARIANT',
?,
'SUCCEEDED',
'{"source":"target-new"}',
?)
`, targetID, targetID, now); err != nil {
		t.Fatal(err)
	}
	reconnectRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/jobs/"+targetID+"/events", nil)
	reconnectRequest.Header.Set("Last-Event-ID", "2")
	reconnect := httptest.NewRecorder()
	server.Handler().ServeHTTP(reconnect, reconnectRequest)
	if reconnect.Code != http.StatusOK || !strings.Contains(reconnect.Body.String(), "id: 3\nevent: succeeded") ||
		!strings.Contains(reconnect.Body.String(), "target-new") ||
		strings.Contains(reconnect.Body.String(), "event: snapshot") {
		t.Fatalf("reconnected stream = %d %s", reconnect.Code, reconnect.Body.String())
	}
	invalidRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/jobs/"+targetID+"/events", nil)
	invalidRequest.Header.Set("Last-Event-ID", "4")
	invalid := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalid, invalidRequest)
	if invalid.Code != http.StatusBadRequest ||
		!strings.Contains(invalid.Body.String(), `"code":"INVALID_EVENT_CURSOR"`) {
		t.Fatalf("invalid cursor = %d %s", invalid.Code, invalid.Body.String())
	}

	runningID := "01980000-0000-7000-8000-000000000083"
	if _, err := server.database.Exec(`
INSERT INTO jobs(id,
scope_type,
scope_id,
kind,
dedupe_key,
execution_no,
payload_json,
cancellable,
state,
attempt_count,
max_attempts,
available_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
'GAME_VARIANT',
?,
'VARIANT_REVALIDATE',
?,
1,
'{}',
0,
'RUNNING',
1,
2,
?,
?,
?)
`, runningID, runningID, strings.Repeat("c", 64), now, now, now); err != nil {
		t.Fatal(err)
	}
	server.sseHeartbeat = 5 * time.Millisecond
	heartbeatContext, cancelHeartbeat := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancelHeartbeat()
	heartbeatRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/jobs/"+runningID+"/events", nil).
		WithContext(heartbeatContext)
	heartbeat := httptest.NewRecorder()
	server.Handler().ServeHTTP(heartbeat, heartbeatRequest)
	if heartbeat.Code != http.StatusOK || !strings.Contains(heartbeat.Body.String(), ": heartbeat\n\n") {
		t.Fatalf("heartbeat stream = %d %s", heartbeat.Code, heartbeat.Body.String())
	}
}

func setCSRFCredentials(request *http.Request, cookie *http.Cookie, token string) {
	request.AddCookie(cookie)
	request.Header.Set("Origin", "http://localhost:3000")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("X-Retrom-Csrf", token)
}

func TestParseETag(t *testing.T) {
	t.Parallel()
	if version, err := ParseETag(`"v1"`); err != nil || version != 1 {
		t.Fatalf("ParseETag minimum = %d, %v", version, err)
	}
	if version, err := ParseETag(`"v42"`); err != nil || version != 42 {
		t.Fatalf("ParseETag = %d, %v", version, err)
	}
	for _, invalid := range []string{"v42", `W/"v42"`, `"v0"`, `"v042"`, `"x1"`} {
		if _, err := ParseETag(invalid); err == nil {
			t.Fatalf("ParseETag(%q) succeeded", invalid)
		}
	}
}

func TestDecodeJSONRejectsDuplicateInvalidUTF8AndDeepValues(t *testing.T) {
	t.Parallel()
	for name, contents := range map[string][]byte{
		"duplicate":    []byte(`{"name":"one","name":"two"}`),
		"invalid utf8": {'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'},
		"trailing":     []byte(`{} {}`),
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(contents))
			request.Header.Set("Content-Type", "application/json")
			if err := decodeJSON(httptest.NewRecorder(), request, &map[string]any{}, 4096); err == nil {
				t.Fatal("decodeJSON accepted malformed JSON")
			}
		})
	}
	deep := bytes.Repeat([]byte{'['}, 65)
	deep = append(deep, '0')
	deep = append(deep, bytes.Repeat([]byte{']'}, 65)...)
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(deep))
	request.Header.Set("Content-Type", "application/json")
	if err := decodeJSON(httptest.NewRecorder(), request, &[]any{}, 4096); err == nil {
		t.Fatal("decodeJSON accepted depth 65")
	}
}

func TestGameMetadataPatchDistinguishesNullFromAbsent(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"players":null,"releaseYear":1993}`))
	request.Header.Set("Content-Type", "application/json")
	var body patchGameRequest
	if err := decodeJSON(httptest.NewRecorder(), request, &body, 4096); err != nil {
		t.Fatal(err)
	}
	if !body.Players.Present || body.Players.Value != nil || !body.ReleaseYear.Present ||
		body.ReleaseYear.Value == nil ||
		*body.ReleaseYear.Value != 1993 ||
		!validPatchGame(body, time.Now()) {
		t.Fatalf("nullable patch = %#v", body)
	}
	if validPatchGame(patchGameRequest{}, time.Now()) {
		t.Fatal("empty metadata patch accepted")
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatalf("load dependencies: %v", err)
	}
	origin, _ := url.Parse("http://localhost:3000")
	dataDir := t.TempDir()
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatalf("open blobs: %v", err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatalf("create credentials: %v", err)
	}
	return New(
		config.Config{PublicOrigin: origin, ActiveEJSVersion: "4.2.3", DataDir: dataDir},
		database.SQL,
		dependencySet,
		blobs,
		credentials,
		time.Now,
	)
}
