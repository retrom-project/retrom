package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/testassert"
)

func TestBlockedReviewDetailRemainsVisibleWithoutSelectedValidation(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	now := time.Now()
	if err := server.dependencies.Bootstrap(context.Background(), server.database, now); err != nil {
		t.Fatal(err)
	}
	var artifactID string
	if err := server.database.QueryRowContext(context.Background(), `
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
	sourceSnapshotID := "01980000-0000-7000-8000-000000000137"
	digest := strings.Repeat("a", 64)
	timestamp := now.UnixMilli()
	coverPayload, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	testassert.False(t, err != nil, err)
	coverMetadata, err := server.blobs.Put(bytes.NewReader(coverPayload))
	testassert.False(t, err != nil, err)
	transaction, err := server.database.BeginTx(context.Background(), nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	manifest := `{"files":[{"logicalName":"blocked.gba","role":"CONTENT"}]}`
	seedReviewSources(t, transaction, uploadID, digest, importID, artifactID, itemID, sourceBlobID, coverBlobID, uploadFileID, coverUploadFileID, sourceSnapshotID, manifest, timestamp, coverMetadata)
	seedReviewValidation(t, transaction, validationID, itemID, artifactID, digest, sourceSnapshotID, draftID, scrapeJobID, timestamp)
	seedReviewMetadataEvidence(t, transaction, scrapeRunID, itemID, scrapeJobID, providerResponseID, candidateID, candidateAssetID, readyCoverAssetID, coverBlobID, digest, timestamp)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/reviews/"+itemID, nil)
	request.SetPathValue("importItemId", itemID)
	server.review(recorder, request)
	testassert.Falsef(t, testassert.Any(func() bool { return recorder.Code != http.StatusOK }, func() bool { return !strings.Contains(recorder.Body.String(), `"status":"BLOCKED"`) }, func() bool { return !strings.Contains(recorder.Body.String(), `"current":false`) }, func() bool {
		return !strings.Contains(recorder.Body.String(), `"compatibilityCode":"DEPENDENCY_MISSING"`)
	}, func() bool { return !strings.Contains(recorder.Body.String(), `"title":"Visible candidate"`) }, func() bool { return !strings.Contains(recorder.Body.String(), `"errorCode":"ASSET_HTTP_STATUS"`) }, func() bool { return !strings.Contains(recorder.Body.String(), `"name":"blocked.zip"`) }, func() bool { return !strings.Contains(recorder.Body.String(), `"archive":true`) }, func() bool {
		return !strings.Contains(recorder.Body.String(), `"archiveEntries":[{"crc32":"`+strings.Repeat("e", 8)+`","name":"blocked.gba","sizeBytes":4096}]`)
	}, func() bool {
		return !strings.Contains(recorder.Body.String(), `"scrapeRuns":[{"attemptCount":0,"candidateCount":1,"completedAtMs":`)
	}, func() bool { return !strings.Contains(recorder.Body.String(), `"provider":"HASHEOUS"`) }), "blocked review detail = %d %s", recorder.Code, recorder.Body.String())
	uploadedCover := httptest.NewRecorder()
	invalidCoverRequest := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/admin/reviews/"+itemID+"/assets", strings.NewReader(`{"uploadFileId":"`+coverUploadFileID+`","kind":"BACKGROUND"}`))
	invalidCoverRequest.SetPathValue("importItemId", itemID)
	invalidCoverRequest.Header.Set("Content-Type", "application/json")
	invalidCoverRequest.Header.Set("If-Match", `"v1"`)
	invalidCoverRequest.Header.Set("Idempotency-Key", uuid.NewString())
	server.createReviewAsset(uploadedCover, invalidCoverRequest)
	testassert.Falsef(t, anyTrue(uploadedCover.Code != http.StatusBadRequest,
		!strings.Contains(uploadedCover.Body.String(), `"code":"INVALID_REQUEST"`)),
		"reject non-cover review asset = %d %s", uploadedCover.Code, uploadedCover.Body.String())
	uploadedCoverAssetID := createReviewCoverFixture(t, server, itemID, coverUploadFileID)
	patch := httptest.NewRecorder()
	patchRequest := httptest.NewRequestWithContext(context.Background(), http.MethodPatch, "/api/v1/admin/reviews/"+itemID, strings.NewReader(`{"selectedAssets":{"coverCandidateAssetId":null,"coverUploadedAssetId":"`+uploadedCoverAssetID+`","backgroundCandidateAssetId":null,"screenshotCandidateAssetIds":[]},"tagIds":[]}`))
	patchRequest.SetPathValue("importItemId", itemID)
	patchRequest.Header.Set("Content-Type", "application/json")
	patchRequest.Header.Set("If-Match", `"v1"`)
	server.patchReview(patch, patchRequest)
	testassert.Falsef(t, anyTrue(patch.Code != http.StatusOK, !strings.Contains(patch.Body.String(), `"version":2`)),
		"select review cover = %d %s", patch.Code, patch.Body.String())
	staleCover := httptest.NewRecorder()
	staleCoverRequest := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/admin/reviews/"+itemID+"/assets", strings.NewReader(`{"uploadFileId":"`+coverUploadFileID+`","kind":"COVER"}`))
	staleCoverRequest.SetPathValue("importItemId", itemID)
	staleCoverRequest.Header.Set("Content-Type", "application/json")
	staleCoverRequest.Header.Set("If-Match", `"v1"`)
	staleCoverRequest.Header.Set("Idempotency-Key", uuid.NewString())
	server.createReviewAsset(staleCover, staleCoverRequest)
	testassert.Falsef(t, anyTrue(staleCover.Code != http.StatusConflict,
		!strings.Contains(staleCover.Body.String(), `"code":"REVIEW_VERSION_CONFLICT"`)),
		"stale review cover upload = %d %s", staleCover.Code, staleCover.Body.String())
	if _, err := server.database.ExecContext(context.Background(), `
UPDATE review_drafts SET cover_candidate_asset_id=? WHERE import_item_id=?
`, readyCoverAssetID, itemID); err == nil || !strings.Contains(err.Error(), "invalid review uploaded cover") {
		t.Fatalf("manual and candidate cover database invariant error = %v", err)
	}
	list := httptest.NewRecorder()
	server.reviews(list, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/reviews", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return list.Code != http.StatusOK }, func() bool { return !strings.Contains(list.Body.String(), `"sourceTotalSizeBytes":4096`) }, func() bool { return !strings.Contains(list.Body.String(), `"sourceMd5":"`+strings.Repeat("c", 32)+`"`) }, func() bool {
		return !strings.Contains(list.Body.String(), `"coverUrl":"/api/v1/admin/review-assets/`+uploadedCoverAssetID+`"`)
	}), "review queue source projection = %d %s", list.Code, list.Body.String())
	filteredList := httptest.NewRecorder()
	server.reviews(
		filteredList, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/reviews?importJobId="+importID, nil),
	)
	testassert.Falsef(t, anyTrue(filteredList.Code != http.StatusOK,
		!strings.Contains(filteredList.Body.String(), `"itemId":"`+itemID+`"`)),
		"review queue import filter = %d %s", filteredList.Code, filteredList.Body.String())
	assertPegasusReviewSources(
		t, server, itemID, importID, artifactID, coverBlobID, readyCoverAssetID, uploadedCoverAssetID,
		manifest, digest, timestamp, coverMetadata, coverPayload,
	)
}

func createReviewCoverFixture(t *testing.T, server *Server, itemID, uploadFileID string) string {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/api/v1/admin/reviews/"+itemID+"/assets",
		strings.NewReader(`{"uploadFileId":"`+uploadFileID+`","kind":"COVER"}`))
	request.SetPathValue("importItemId", itemID)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"v1"`)
	request.Header.Set("Idempotency-Key", uuid.NewString())
	server.createReviewAsset(response, request)
	testassert.Falsef(t, anyTrue(response.Code != http.StatusCreated,
		!strings.Contains(response.Body.String(), `"url":"/api/v1/admin/review-assets/`)),
		"create review cover = %d %s", response.Code, response.Body.String())
	var result struct {
		AssetID string `json:"assetId"`
	}
	mustDecodeHTTPTest(t, response.Body.Bytes(), &result)
	return result.AssetID
}

func assertPegasusReviewSources(
	t *testing.T,
	server *Server,
	itemID, importID, artifactID, coverBlobID, readyCoverAssetID, uploadedCoverAssetID string,
	manifest, digest string,
	timestamp int64,
	coverMetadata blobstore.Metadata,
	coverPayload []byte,
) {
	pegasusImportID := "01980000-0000-7000-8000-000000000138"
	pegasusScanJobID := "01980000-0000-7000-8000-000000000139"
	pegasusWorkJobID := "01980000-0000-7000-8000-000000000140"
	pegasusCollectionID := "01980000-0000-7000-8000-000000000141"
	pegasusItemID := "01980000-0000-7000-8000-000000000142"
	pegasusVideoBlobID := "01980000-0000-7000-8000-000000000143"
	videoPayload := []byte("pegasus review video fixture")
	videoMetadata, err := server.blobs.Put(bytes.NewReader(videoPayload))
	testassert.False(t, err != nil, err)
	mustExecHTTPTest(t, server.database, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,finished_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'SERVER_PEGASUS_SCAN',?,1,'{}',1,'SUCCEEDED',1,4,1,?,?,?,?),
      (?,'PEGASUS_IMPORT',?,'SERVER_PEGASUS_IMPORT',?,1,'{}',1,'SUCCEEDED',1,4,1,?,?,?,?)
`, pegasusScanJobID, pegasusImportID, strings.Repeat("1", 64), timestamp, timestamp, timestamp, timestamp,
		pegasusWorkJobID, pegasusImportID, strings.Repeat("2", 64), timestamp, timestamp, timestamp, timestamp,
	)
	mustExecHTTPTest(t, server.database, `
INSERT INTO pegasus_imports(
 id,root_id,root_label_snapshot,source_relative_path,root_config_digest,state,phase,scan_job_id,
 import_job_id,collection_count,game_count,mapped_collection_count,processable_item_count,
 review_pending_item_count,created_by_user_id,created_at_ms,updated_at_ms,scan_completed_at_ms,
 started_at_ms,completed_at_ms,expires_at_ms
) VALUES(?,'games','Games','FC',?,'COMPLETED','PREPARING_REVIEWS',?,?,1,1,1,1,1,
 '01980000-0000-7000-8000-000000009999',?,?,?,?,?,?)
`, pegasusImportID, strings.Repeat("3", 64), pegasusScanJobID, pegasusWorkJobID,
		timestamp, timestamp, timestamp, timestamp, timestamp, timestamp+60_000,
	)
	mustExecHTTPTest(t, server.database, `
INSERT INTO pegasus_import_collections(
 id,import_id,metadata_relative_path,segment_ordinal,name,game_count,mapping_action,
 target_platform_instance_id,target_platform_instance_version,target_platform_id,target_default_core_id,
 target_core_artifact_id,target_core_artifact_version,created_at_ms,updated_at_ms
) VALUES(?,?,'FC/metadata.pegasus.txt',0,'FC',1,'IMPORT',
 (SELECT id FROM platform_instances WHERE catalog_template_key='gba/mgba'),1,'gba','mgba',?,1,?,?)
`, pegasusCollectionID, pegasusImportID, artifactID, timestamp, timestamp)
	mustExecHTTPTest(t, server.database, `
INSERT INTO pegasus_import_items(
 id,import_id,collection_id,metadata_relative_path,game_ordinal,source_key,title,discovery_state,
 execution_state,content_kind,metadata_json,source_manifest_json,source_manifest_digest,
 library_import_job_id,library_import_item_id,created_at_ms,updated_at_ms,completed_at_ms
) VALUES(?,?,?,'FC/metadata.pegasus.txt',0,?,'Pegasus source title','READY','REVIEW_PENDING',
 'SINGLE_FILE','{"title":"Pegasus source title"}',?,?,?,?,?,?,?)
`, pegasusItemID, pegasusImportID, pegasusCollectionID, strings.Repeat("4", 64), manifest, digest,
		importID, itemID, timestamp, timestamp, timestamp,
	)
	mustExecHTTPTest(t, server.database, `
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES(?,?,?,?,?,?,'video/mp4',?)
`, pegasusVideoBlobID, videoMetadata.SHA256, videoMetadata.Size, videoMetadata.MD5,
		videoMetadata.SHA1, videoMetadata.CRC32, timestamp,
	)
	mustExecHTTPTest(t, server.database, `
INSERT INTO pegasus_import_item_assets(
 item_id,kind,resolution_method,relative_path,size_bytes,source_facts_digest,blob_id,media_type,
 width_px,height_px,state,created_at_ms,updated_at_ms
) VALUES(?,'COVER','EXPLICIT_GAME','FC/media/cover.png',?,?,?,'image/png',1,1,'COPIED',?,?),
        (?,'VIDEO','EXPLICIT_GAME','FC/media/video.mp4',?,?,?,'video/mp4',NULL,NULL,'COPIED',?,?)
`, pegasusItemID, coverMetadata.Size, strings.Repeat("5", 64), coverBlobID, timestamp, timestamp,
		pegasusItemID, videoMetadata.Size, strings.Repeat("6", 64), pegasusVideoBlobID, timestamp, timestamp,
	)
	mustExecHTTPTest(t, server.database, `
UPDATE pegasus_import_items
SET execution_state='VALIDATING',completed_at_ms=NULL
WHERE id=?
`, pegasusItemID)
	hiddenPegasusList := httptest.NewRecorder()
	server.reviews(
		hiddenPegasusList,
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/reviews?pegasusImportId="+pegasusImportID, nil),
	)
	testassert.Falsef(t, anyTrue(hiddenPegasusList.Code != http.StatusOK,
		strings.Contains(hiddenPegasusList.Body.String(), itemID)),
		"incomplete Pegasus handoff leaked into review queue = %d %s", hiddenPegasusList.Code, hiddenPegasusList.Body.String())
	hiddenPegasusDetail := httptest.NewRecorder()
	hiddenPegasusDetailRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/reviews/"+itemID, nil)
	hiddenPegasusDetailRequest.SetPathValue("importItemId", itemID)
	server.review(hiddenPegasusDetail, hiddenPegasusDetailRequest)
	testassert.Falsef(t, hiddenPegasusDetail.Code != http.StatusNotFound, "incomplete Pegasus handoff detail = %d %s", hiddenPegasusDetail.Code, hiddenPegasusDetail.Body.String())
	mustExecHTTPTest(t, server.database, `
UPDATE pegasus_import_items
SET execution_state='REVIEW_PENDING',completed_at_ms=?
WHERE id=?
`, timestamp, pegasusItemID)
	pegasusList := httptest.NewRecorder()
	server.reviews(
		pegasusList,
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/reviews?pegasusImportId="+pegasusImportID, nil),
	)
	testassert.Falsef(t, testassert.Any(func() bool { return pegasusList.Code != http.StatusOK }, func() bool { return !strings.Contains(pegasusList.Body.String(), `"itemId":"`+itemID+`"`) }, func() bool { return !strings.Contains(pegasusList.Body.String(), `"sourceKind":"PEGASUS"`) }, func() bool { return !strings.Contains(pegasusList.Body.String(), `"sourceLabel":"FC"`) }, func() bool {
		return !strings.Contains(pegasusList.Body.String(), `"pegasusImportId":"`+pegasusImportID+`"`)
	}), "Pegasus review queue filter = %d %s", pegasusList.Code, pegasusList.Body.String())
	pegasusDetail := httptest.NewRecorder()
	pegasusDetailRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/reviews/"+itemID, nil)
	pegasusDetailRequest.SetPathValue("importItemId", itemID)
	server.review(pegasusDetail, pegasusDetailRequest)
	testassert.Falsef(t, testassert.Any(func() bool { return pegasusDetail.Code != http.StatusOK }, func() bool { return !strings.Contains(pegasusDetail.Body.String(), `"sourceKind":"PEGASUS"`) }, func() bool {
		return !strings.Contains(pegasusDetail.Body.String(), `"coverUrl":"/api/v1/admin/review-assets/`+pegasusItemID+`?kind=COVER"`)
	}, func() bool {
		return !strings.Contains(pegasusDetail.Body.String(), `"videoUrl":"/api/v1/admin/review-assets/`+pegasusItemID+`?kind=VIDEO"`)
	}), "Pegasus review source media = %d %s", pegasusDetail.Code, pegasusDetail.Body.String())
	pegasusVideo := httptest.NewRecorder()
	pegasusVideoRequest := httptest.NewRequestWithContext(context.Background(),
		http.MethodGet,
		"/api/v1/admin/review-assets/"+pegasusItemID+"?kind=VIDEO",
		nil,
	)
	pegasusVideoRequest.SetPathValue("assetId", pegasusItemID)
	server.reviewCandidateAsset(pegasusVideo, pegasusVideoRequest)
	testassert.Falsef(t, anyTrue(pegasusVideo.Code != http.StatusOK,
		!bytes.Equal(pegasusVideo.Body.Bytes(), videoPayload), pegasusVideo.Header().Get("Content-Type") != "video/mp4"),
		"Pegasus review video = %d/%s %q", pegasusVideo.Code, pegasusVideo.Header().Get("Content-Type"), pegasusVideo.Body.Bytes())
	assertHistoricalReviewAssets(
		t, server, itemID, pegasusItemID, readyCoverAssetID, uploadedCoverAssetID, timestamp, coverPayload,
	)
}

func assertHistoricalReviewAssets(
	t *testing.T, server *Server, itemID, pegasusItemID, readyCoverAssetID, uploadedCoverAssetID string,
	timestamp int64, coverPayload []byte,
) {
	mustExecHTTPTest(t, server.database, `
UPDATE import_items SET state='PUBLISHED' WHERE id=?;
INSERT INTO review_events(id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,before_json,after_json,diff_json,
config_evidence_json,dat_evidence_json,provider_evidence_json,reason,created_at_ms)
VALUES('01980000-0000-7000-8000-000000000135',?,'APPROVED','SYSTEM',NULL,'release-setup',?,
'{"schemaVersion":1,"decision":"APPROVED"}','{}','{}','{}','{}',NULL,?)
`, itemID, `{"schemaVersion":1,"selectedAssets":{"coverCandidateAssetId":null,"coverUploadedAssetId":null}}`, timestamp)
	historyDetail := httptest.NewRecorder()
	historyRequest := httptest.NewRequestWithContext(context.Background(),
		http.MethodGet,
		"/api/v1/admin/review-history/01980000-0000-7000-8000-000000000135",
		nil,
	)
	historyRequest.SetPathValue("reviewEventId", "01980000-0000-7000-8000-000000000135")
	server.reviewHistoryEvent(historyDetail, historyRequest)
	testassert.Falsef(t, testassert.Any(func() bool { return historyDetail.Code != http.StatusOK }, func() bool {
		return !strings.Contains(historyDetail.Body.String(), `"actor":{"kind":"SYSTEM","label":"release-setup","userId":null}`)
	}, func() bool {
		return !strings.Contains(historyDetail.Body.String(), `"before":{"schemaVersion":1,"selectedAssets"`)
	}, func() bool {
		return !strings.Contains(historyDetail.Body.String(), `"coverUrl":"/api/v1/admin/review-assets/`+pegasusItemID+`?kind=COVER"`)
	}), "review history detail = %d %s", historyDetail.Code, historyDetail.Body.String())
	historicalCover := httptest.NewRecorder()
	historicalRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/review-assets/"+readyCoverAssetID, nil)
	historicalRequest.SetPathValue("assetId", readyCoverAssetID)
	server.reviewCandidateAsset(historicalCover, historicalRequest)
	testassert.Falsef(t, anyTrue(historicalCover.Code != http.StatusOK,
		!bytes.Equal(historicalCover.Body.Bytes(), coverPayload)),
		"historical review cover = %d %q", historicalCover.Code, historicalCover.Body.Bytes())
	historicalUploadedCover := httptest.NewRecorder()
	historicalUploadedRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/review-assets/"+uploadedCoverAssetID, nil)
	historicalUploadedRequest.SetPathValue("assetId", uploadedCoverAssetID)
	server.reviewCandidateAsset(historicalUploadedCover, historicalUploadedRequest)
	testassert.Falsef(t, anyTrue(historicalUploadedCover.Code != http.StatusOK,
		!bytes.Equal(historicalUploadedCover.Body.Bytes(), coverPayload)),
		"historical uploaded review cover = %d %q", historicalUploadedCover.Code, historicalUploadedCover.Body.Bytes())
}

func seedReviewSources(
	t *testing.T, transaction *sql.Tx,
	uploadID, digest, importID, artifactID, itemID, sourceBlobID, coverBlobID string,
	uploadFileID, coverUploadFileID, sourceSnapshotID, manifest string,
	timestamp int64, coverMetadata blobstore.Metadata,
) {
	mustExecHTTPTest(t, transaction, `
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
`, uploadID, digest, timestamp+60_000, timestamp, timestamp)
	mustExecHTTPTest(t, transaction, `
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
(SELECT id FROM platform_instances WHERE catalog_template_key='gba/mgba'),
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
`, importID, uploadID, artifactID, digest, timestamp, timestamp)
	mustExecHTTPTest(t, transaction, `
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
`, itemID, importID, digest, manifest, digest, timestamp, timestamp)
	mustExecHTTPTest(t, transaction, `
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms) VALUES
(?,?,4096,?,?,?,'application/octet-stream',?),
(?,?,?,?,?,?,'image/png',?)
`, sourceBlobID, strings.Repeat("b", 64), strings.Repeat("c", 32), strings.Repeat("d", 40), strings.Repeat("e", 8), timestamp,
		coverBlobID, coverMetadata.SHA256, coverMetadata.Size, coverMetadata.MD5, coverMetadata.SHA1, coverMetadata.CRC32, timestamp)
	mustExecHTTPTest(t, transaction, `
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,final_blob_id,state,created_at_ms,updated_at_ms)
VALUES(?,?, 'blocked.zip',4096,4096,?,'COMPLETE',?,?),
(?,?,'manual-cover.png',?,?,?,'COMPLETE',?,?)
	`, uploadFileID, uploadID, sourceBlobID, timestamp, timestamp,
		coverUploadFileID, uploadID, coverMetadata.Size, coverMetadata.Size, coverBlobID, timestamp, timestamp)
	mustExecHTTPTest(t, transaction, `
INSERT INTO archive_entries(archive_blob_id,ordinal,original_relative_path,normalized_path,ascii_casefold_path,
archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,materialized_blob_id,created_at_ms)
VALUES(?,0,'blocked.gba','blocked.gba','blocked.gba','ZIP','DEFLATE',4096,?,?,?,?,?,?)
	`, sourceBlobID, strings.Repeat("e", 8), strings.Repeat("c", 32), strings.Repeat("d", 40), strings.Repeat("b", 64), sourceBlobID, timestamp)
	mustExecHTTPTest(t, transaction, `
INSERT INTO import_item_source_files(import_item_id,role,logical_name,upload_file_id,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order,created_at_ms)
VALUES(?,'CONTENT','blocked.zip',?,?,NULL,NULL,0,?)
	`, itemID, uploadFileID, sourceBlobID, timestamp)
	mustExecHTTPTest(t, transaction, `
INSERT INTO import_item_source_snapshots(id,import_item_id,revision_no,source_manifest_json,
source_manifest_digest,created_by,created_at_ms)
VALUES(?,?,1,?,?,'IDENTIFICATION',?)
	`, sourceSnapshotID, itemID, manifest, digest, timestamp)
	mustExecHTTPTest(t, transaction, `
INSERT INTO import_item_source_snapshot_files(source_snapshot_id,role,logical_name,upload_file_id,
blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order,created_at_ms)
VALUES(?,'CONTENT','blocked.zip',?,?,NULL,NULL,0,?)
	`, sourceSnapshotID, uploadFileID, sourceBlobID, timestamp)
}

func seedReviewValidation(
	t *testing.T, transaction *sql.Tx,
	validationID, itemID, artifactID, digest, sourceSnapshotID, draftID, scrapeJobID string,
	timestamp int64,
) {
	mustExecHTTPTest(t, transaction, `
INSERT INTO import_item_core_validations(id,
import_item_id,
target_platform_instance_id,
platform_instance_version,
core_id,
core_artifact_id,
source_manifest_digest,
source_snapshot_id,
prepublish_input_digest,
status,
compatibility_code,
dependency_snapshot_json,
created_at_ms) VALUES(?,
?,
(SELECT id FROM platform_instances WHERE catalog_template_key='gba/mgba'),
1,
'mgba',
?,
?,
?,
?,
'BLOCKED',
'DEPENDENCY_MISSING',
'{"dependencies":[]}',
?)
`, validationID, itemID, artifactID, digest, sourceSnapshotID, digest, timestamp)
	mustExecHTTPTest(t, transaction, `
INSERT INTO review_drafts(id,
import_item_id,
target_platform_instance_id,
selected_validation_id,
effective_source_snapshot_id,
metadata_json,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
(SELECT id FROM platform_instances WHERE catalog_template_key='gba/mgba'),
NULL,
?,
'{"title":"Blocked","description":"","developer":"","publisher":"","genre":"","players":null,"releaseYear":null}',
1,
?,
?)
`, draftID, itemID, sourceSnapshotID, timestamp, timestamp)
	mustExecHTTPTest(t, transaction, `
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
`, scrapeJobID, itemID, digest, timestamp, timestamp, timestamp, timestamp)
}

func seedReviewMetadataEvidence(
	t *testing.T, transaction *sql.Tx,
	scrapeRunID, itemID, scrapeJobID, providerResponseID, candidateID, candidateAssetID string,
	readyCoverAssetID, coverBlobID, digest string, timestamp int64,
) {
	mustExecHTTPTest(t, transaction, `
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
`, scrapeRunID, itemID, scrapeJobID, timestamp, timestamp, timestamp)
	mustExecHTTPTest(t, transaction, `
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
`, providerResponseID, digest, timestamp, timestamp)
	mustExecHTTPTest(t, transaction, `
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
`, candidateID, scrapeRunID, providerResponseID, timestamp)
	mustExecHTTPTest(t, transaction, `
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
`, candidateAssetID, candidateID, providerResponseID, timestamp, timestamp)
	mustExecHTTPTest(t, transaction, `
INSERT INTO scrape_candidate_assets(id,scrape_candidate_id,provider_response_id,provider_asset_id,kind_hint,ordinal,
source_path,status,blob_id,width_px,height_px,media_type,fetched_at_ms,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,'cover-ready','COVER',1,'/api/v1/images/cover-ready','READY',?,600,800,'image/png',?,1,?,?)
`, readyCoverAssetID, candidateID, providerResponseID, coverBlobID, timestamp, timestamp, timestamp)
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
}
