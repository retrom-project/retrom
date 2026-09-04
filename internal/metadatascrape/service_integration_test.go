//go:build integration

package metadatascrape_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/hasheous"
	"retrom/internal/legacychecksum"
	"retrom/internal/libraryimport"
	"retrom/internal/metadatascrape"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (function doerFunc) Do(request *http.Request) (*http.Response, error) { return function(request) }

type resolverFunc func(context.Context, string) ([]net.IPAddr, error)

func (function resolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return function(ctx, host)
}

func TestImportPersistsHasheousEvidenceCandidateAndAsset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	contents := []byte("deterministic metadata fixture")
	legacyMD5, legacySHA1 := legacychecksum.Sum(contents)
	upload, err := uploadService.Create(
		ctx,
		uploads.CreateRequest{
			SourceType: "FILES",
			Files: []uploads.FileDeclaration{
				{ClientFileID: "game", RelativePath: "Metadata.gba", SizeBytes: int64(len(contents))},
			},
		},
	)
	testassert.False(t, err != nil, err)
	digest := sha256.Sum256(contents)
	digestHeader := "sha-256=:" + base64.StdEncoding.EncodeToString(digest[:]) + ":"
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0, fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)), digestHeader, bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	current, _ := uploadService.Get(ctx, upload.ID)
	jobID, _, err := uploadService.Complete(ctx, upload.ID, current.Version)
	testassert.False(t, err != nil, err)
	waitForState(t, database.SQL.QueryRowContext, `
SELECT state
FROM jobs
WHERE id=?
`, jobID, "SUCCEEDED")
	pngBytes, _ := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	)
	var lookupCount atomic.Int64
	lookupGate := make(chan struct{})
	client := doerFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/api/v1/Lookup/ByHash" {
			<-lookupGate
			body, readErr := io.ReadAll(request.Body)
			var hashes map[string]string
			testassert.CheckFalsef(t, testassert.Any(func() bool { return readErr != nil }, func() bool { return json.Unmarshal(body, &hashes) != nil }, func() bool { return len(hashes) != 4 }, func() bool { return hashes["crc"] != fmt.Sprintf("%08x", crc32.ChecksumIEEE(contents)) }, func() bool { return hashes["mD5"] != legacyMD5 }, func() bool { return hashes["shA1"] != legacySHA1 }, func() bool { return hashes["shA256"] != fmt.Sprintf("%x", sha256.Sum256(contents)) }), "raw/member lookup body = %s, read error=%v", body, readErr)
			if lookupCount.Add(1) == 1 {
				return httpResponse(http.StatusTooManyRequests, "text/plain", "retry"), nil
			}
			return httpResponse(
				http.StatusOK,
				"application/json",
				`{"id":73,"name":"Metadata Result","platform":{"name":"Game Boy Advance"},"signature":{"game":{"description":"safe","year":"2002"}},"attributes":[{"attributeName":"Logo","attributeType":"ImageId","attributeRelationType":"None","value":"logo","link":"/api/v1/images/logo"},{"attributeName":"Tags","attributeType":"EmbeddedList","attributeRelationType":"None","value":{"GameGenre":{"Tags":[{"Text":"action"}]}}}]}`,
			), nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(bytes.NewReader(pngBytes)),
		}, nil
	})
	resolver := resolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	})
	scraper := metadatascrape.New(database.SQL, blobs, hasheous.New(client, resolver, time.Now), time.Now)
	importer := libraryimport.New(database.SQL, time.Now, scraper).WithBlobStore(blobs)
	created, err := importer.Create(
		ctx,
		libraryimport.CreateRequest{
			UploadID:                 upload.ID,
			TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "gba/mgba"),
			MetadataProvider:         "HASHEOUS",
		},
	)
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, created.State != "RUNNING", "initial import state = %s", created.State)
	var initialItemState, initialJobState string
	var initialRunning, initialReviewPending int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT i.state,
j.state,
j.running_item_count,
j.review_pending_item_count
FROM import_items i
JOIN import_jobs j ON j.id=i.import_job_id
WHERE j.id=?
`, created.ImportJobID).Scan(
		&initialItemState,
		&initialJobState,
		&initialRunning,
		&initialReviewPending,
	); err != nil || initialItemState != "SCRAPING" || initialJobState != "RUNNING" ||
		initialRunning != 1 || initialReviewPending != 0 {
		t.Fatalf(
			"initial item/job state = %s/%s running=%d review=%d, error=%v",
			initialItemState,
			initialJobState,
			initialRunning,
			initialReviewPending,
			err,
		)
	}
	close(lookupGate)
	var scrapeJobID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT job_id
FROM metadata_scrape_runs r
JOIN import_items i ON i.id=r.import_item_id
WHERE i.import_job_id=?
`, created.ImportJobID).Scan(&scrapeJobID); err != nil {
		t.Fatal(err)
	}
	waitForState(t, database.SQL.QueryRowContext, `
SELECT state
FROM jobs
WHERE id=?
`, scrapeJobID, "SUCCEEDED")
	var rawProfile, rawCRC32, rawMD5, rawSHA1, rawSHA256 string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT profile,
crc32,
md5,
sha1,
sha256
FROM content_hash_evidence e
JOIN metadata_scrape_runs r ON r.id=e.scrape_run_id
WHERE r.job_id=?
`, scrapeJobID).Scan(&rawProfile, &rawCRC32, &rawMD5, &rawSHA1, &rawSHA256); err != nil ||
		rawProfile != "RAW_FILE" ||
		rawCRC32 != fmt.Sprintf("%08x", crc32.ChecksumIEEE(contents)) ||
		rawMD5 != legacyMD5 ||
		rawSHA1 != legacySHA1 ||
		rawSHA256 != fmt.Sprintf("%x", sha256.Sum256(contents)) {
		t.Fatalf("raw evidence = %s %s %s %s %s, error=%v", rawProfile, rawCRC32, rawMD5, rawSHA1, rawSHA256, err)
	}
	var candidates, attempts, readyAssets, rawResponses int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT (SELECT count(*)
FROM scrape_candidates c
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
WHERE r.job_id=?),
(SELECT count(*)
FROM metadata_scrape_query_attempts a
JOIN metadata_scrape_runs r ON r.id=a.scrape_run_id
WHERE r.job_id=?),
(SELECT count(*)
FROM scrape_candidate_assets a
JOIN scrape_candidates c ON c.id=a.scrape_candidate_id
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
WHERE r.job_id=?
AND a.status='READY'),
(SELECT count(*)
FROM metadata_provider_responses p
JOIN metadata_scrape_query_attempts a ON a.provider_response_id=p.id
JOIN metadata_scrape_runs r ON r.id=a.scrape_run_id
WHERE r.job_id=?
AND p.raw_response_blob_id IS NOT NULL)
`, scrapeJobID, scrapeJobID, scrapeJobID, scrapeJobID).Scan(
		&candidates,
		&attempts,
		&readyAssets,
		&rawResponses,
	); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return lookupCount.Load() != 2 }, func() bool { return candidates != 1 }, func() bool { return attempts != 2 }, func() bool { return readyAssets != 1 }, func() bool { return rawResponses != 2 }), "lookup/candidates/attempts/assets/raw = %d/%d/%d/%d/%d", lookupCount.Load(), candidates, attempts, readyAssets, rawResponses)
	var firstItemID, candidateID, candidateAssetID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT i.id,
c.id,
a.id
FROM import_items i
JOIN metadata_scrape_runs r ON r.import_item_id=i.id
JOIN scrape_candidates c ON c.scrape_run_id=r.id
JOIN scrape_candidate_assets a ON a.scrape_candidate_id=c.id
WHERE i.import_job_id=?
AND a.status='READY'
`, created.ImportJobID).Scan(&firstItemID, &candidateID, &candidateAssetID); err != nil {
		t.Fatal(err)
	}
	var finalItemState, finalJobState, selectedCandidateID, selectedCoverID, metadataJSON string
	var finalRunning, finalReviewPending int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT i.state,
j.state,
j.running_item_count,
j.review_pending_item_count,
d.selected_candidate_id,
d.cover_candidate_asset_id,
d.metadata_json
FROM import_items i
JOIN import_jobs j ON j.id=i.import_job_id
JOIN review_drafts d ON d.import_item_id=i.id
WHERE i.id=?
`, firstItemID).Scan(
		&finalItemState,
		&finalJobState,
		&finalRunning,
		&finalReviewPending,
		&selectedCandidateID,
		&selectedCoverID,
		&metadataJSON,
	); err != nil || finalItemState != "REVIEW_PENDING" || finalJobState != "REVIEW_PENDING" ||
		finalRunning != 0 || finalReviewPending != 1 || selectedCandidateID != candidateID ||
		selectedCoverID != candidateAssetID || !strings.Contains(metadataJSON, `"title":"Metadata Result"`) ||
		!strings.Contains(metadataJSON, `"description":"safe"`) || !strings.Contains(metadataJSON, `"releaseYear":2002`) {
		t.Fatalf(
			"completed initial review = item=%s job=%s running=%d review=%d candidate=%s cover=%s metadata=%s, error=%v",
			finalItemState,
			finalJobState,
			finalRunning,
			finalReviewPending,
			selectedCandidateID,
			selectedCoverID,
			metadataJSON,
			err,
		)
	}
	archiveContents := makeDeterministicZIP(t, map[string][]byte{"folder/Metadata-copy.gba": contents})
	secondUpload, err := uploadService.Create(
		ctx,
		uploads.CreateRequest{
			SourceType: "FILES",
			Files: []uploads.FileDeclaration{
				{ClientFileID: "game-2", RelativePath: "Metadata-copy.zip", SizeBytes: int64(len(archiveContents))},
			},
		},
	)
	testassert.False(t, err != nil, err)
	archiveDigest := sha256.Sum256(archiveContents)
	archiveDigestHeader := "sha-256=:" + base64.StdEncoding.EncodeToString(archiveDigest[:]) + ":"
	if err := uploadService.PutPart(ctx, secondUpload.ID, secondUpload.Files[0].ID, 0, fmt.Sprintf("bytes 0-%d/%d", len(archiveContents)-1, len(archiveContents)), archiveDigestHeader, bytes.NewReader(archiveContents)); err != nil {
		t.Fatal(err)
	}
	secondCurrent, _ := uploadService.Get(ctx, secondUpload.ID)
	secondFinalizeJob, _, err := uploadService.Complete(ctx, secondUpload.ID, secondCurrent.Version)
	testassert.False(t, err != nil, err)
	waitForState(t, database.SQL.QueryRowContext, `
SELECT state
FROM jobs
WHERE id=?
`, secondFinalizeJob, "SUCCEEDED")
	secondImport, err := importer.Create(
		ctx,
		libraryimport.CreateRequest{
			UploadID:                 secondUpload.ID,
			TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "gba/mgba"),
			MetadataProvider:         "HASHEOUS",
		},
	)
	testassert.False(t, err != nil, err)
	var secondScrapeJobID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT job_id
FROM metadata_scrape_runs r
JOIN import_items i ON i.id=r.import_item_id
WHERE i.import_job_id=?
`, secondImport.ImportJobID).Scan(&secondScrapeJobID); err != nil {
		t.Fatal(err)
	}
	waitForState(t, database.SQL.QueryRowContext, `
SELECT state
FROM jobs
WHERE id=?
`, secondScrapeJobID, "SUCCEEDED")
	var memberProfile string
	var memberArchiveID sql.NullString
	var memberOrdinal sql.NullInt64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT e.profile,
e.archive_blob_id,
e.archive_entry_ordinal
FROM content_hash_evidence e
JOIN metadata_scrape_runs r ON r.id=e.scrape_run_id
WHERE r.job_id=?
`, secondScrapeJobID).Scan(&memberProfile, &memberArchiveID, &memberOrdinal); err != nil ||
		memberProfile != "SINGLE_ARCHIVE_MEMBER" || !memberArchiveID.Valid || !memberOrdinal.Valid {
		t.Fatalf("archive member evidence = %s/%#v/%#v, error=%v", memberProfile, memberArchiveID, memberOrdinal, err)
	}
	var networkAttempts, cacheAttempts, providerResponses int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT
(SELECT count(*)
FROM metadata_scrape_query_attempts
WHERE source='NETWORK'),
(SELECT count(*)
FROM metadata_scrape_query_attempts
WHERE source='CACHE'),
(SELECT count(*)
FROM metadata_provider_responses
WHERE provider='HASHEOUS')
`).Scan(&networkAttempts, &cacheAttempts, &providerResponses); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return lookupCount.Load() != 2 }, func() bool { return networkAttempts != 2 }, func() bool { return cacheAttempts != 1 }, func() bool { return providerResponses != 2 }), "cache reuse lookup/network/cache/responses = %d/%d/%d/%d", lookupCount.Load(), networkAttempts, cacheAttempts, providerResponses)
	reason := "已核对 Hasheous 候选与封面"
	approved, err := importer.ApproveWithReason(ctx, firstItemID, 1, &reason)
	testassert.False(t, err != nil, err)
	var publishedAssets int
	var providerEvidence, storedReason string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT (SELECT count(*)
FROM game_assets
WHERE game_id=?
AND kind='COVER'),
provider_evidence_json,
reason
FROM review_events
WHERE import_item_id=?
AND event_type='APPROVED'
	`, approved.GameID, firstItemID).Scan(&publishedAssets, &providerEvidence, &storedReason); err != nil ||
		publishedAssets != 1 ||
		storedReason != reason ||
		!strings.Contains(providerEvidence, `"candidateSelected":true`) ||
		strings.Contains(providerEvidence, candidateAssetID) {
		t.Fatalf(
			"published review assets/evidence = %d %s %q, error=%v",
			publishedAssets,
			providerEvidence,
			storedReason,
			err,
		)
	}

	failureContents := []byte("deterministic metadata failure fixture")
	failureUpload, err := uploadService.Create(
		ctx,
		uploads.CreateRequest{
			SourceType: "FILES",
			Files: []uploads.FileDeclaration{
				{ClientFileID: "game-failure", RelativePath: "Metadata-failure.gba", SizeBytes: int64(len(failureContents))},
			},
		},
	)
	testassert.False(t, err != nil, err)
	failureDigest := sha256.Sum256(failureContents)
	failureDigestHeader := "sha-256=:" + base64.StdEncoding.EncodeToString(failureDigest[:]) + ":"
	if err := uploadService.PutPart(
		ctx,
		failureUpload.ID,
		failureUpload.Files[0].ID,
		0,
		fmt.Sprintf("bytes 0-%d/%d", len(failureContents)-1, len(failureContents)),
		failureDigestHeader,
		bytes.NewReader(failureContents),
	); err != nil {
		t.Fatal(err)
	}
	failureCurrent, _ := uploadService.Get(ctx, failureUpload.ID)
	failureFinalizeJob, _, err := uploadService.Complete(ctx, failureUpload.ID, failureCurrent.Version)
	testassert.False(t, err != nil, err)
	waitForState(t, database.SQL.QueryRowContext, `
SELECT state
FROM jobs
WHERE id=?
`, failureFinalizeJob, "SUCCEEDED")
	failureImport, err := importer.Create(
		ctx,
		libraryimport.CreateRequest{
			UploadID:                 failureUpload.ID,
			TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "gba/mgba"),
			MetadataProvider:         "NONE",
		},
	)
	testassert.False(t, err != nil, err)
	var failureItemID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id
FROM import_items
WHERE import_job_id=?
`, failureImport.ImportJobID).Scan(&failureItemID); err != nil {
		t.Fatal(err)
	}
	failureTransaction, err := database.SQL.BeginTx(ctx, nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(failureTransaction)
	if _, err := failureTransaction.ExecContext(ctx, `
UPDATE import_items
SET state='SCRAPING'
WHERE id=?
`, failureItemID); err != nil {
		t.Fatal(err)
	}
	if _, err := failureTransaction.ExecContext(ctx, `
UPDATE import_jobs
SET state='RUNNING',
running_item_count=1,
review_pending_item_count=0
WHERE id=?
`, failureImport.ImportJobID); err != nil {
		t.Fatal(err)
	}
	failureScrape, err := scraper.ScheduleImport(ctx, failureTransaction, failureItemID, "HASHEOUS")
	testassert.False(t, err != nil, err)
	if _, err := failureTransaction.ExecContext(ctx, `
UPDATE jobs
SET payload_json='{'
WHERE id=?
`, failureScrape.JobID); err != nil {
		t.Fatal(err)
	}
	if err := failureTransaction.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := scraper.Run(ctx, failureScrape.RunID); err == nil {
		t.Fatal("invalid metadata task payload should fail")
	}
	waitForState(t, database.SQL.QueryRowContext, `
SELECT state
FROM jobs
WHERE id=?
`, failureScrape.JobID, "FAILED")
	var failedItemState, failedImportState, failedCode string
	var failedRunning, failedItems, failedReviewPending int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT i.state,
j.state,
j.running_item_count,
j.failed_item_count,
j.review_pending_item_count,
i.last_error_code
FROM import_items i
JOIN import_jobs j ON j.id=i.import_job_id
WHERE j.id=?
`, failureImport.ImportJobID).Scan(
		&failedItemState,
		&failedImportState,
		&failedRunning,
		&failedItems,
		&failedReviewPending,
		&failedCode,
	); err != nil || failedItemState != "FAILED_RETRYABLE" || failedImportState != "PARTIAL_FAILURE" ||
		failedRunning != 0 || failedItems != 1 || failedReviewPending != 0 || failedCode == "" {
		t.Fatalf(
			"failed initial review = item=%s job=%s running=%d failed=%d review=%d code=%s, error=%v",
			failedItemState,
			failedImportState,
			failedRunning,
			failedItems,
			failedReviewPending,
			failedCode,
			err,
		)
	}
}

func TestArcadeHasheousEvidenceUsesMatchedDATEntriesOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	dummy, err := blobs.Put(bytes.NewReader([]byte("arcade evidence dat")))
	testassert.False(t, err != nil, err)
	target, err := testsupport.LookupRuntimeTarget(ctx, database.SQL, "fbneo")
	testassert.False(t, err != nil, err)
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE dat_versions
SET is_active=0
WHERE provider_id=? AND target_id=?
`, target.ProviderID, target.TargetID); err != nil {
		t.Fatal(err)
	}
	datID := "01980000-0000-7000-8000-000000000231"
	now := time.Now().UnixMilli()
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_versions(id,
core_id,
provider_id,
target_id,
target_contract_sha256,
builtin_relative_path,
sha256,
parser_version,
parse_status,
is_active,
machine_count,
rom_entry_count,
disk_entry_count,
bios_set_count,
default_bios_set_count,
explicit_bios_machine_count,
base_dependency_target_count,
unresolved_relation_count,
version,
created_at_ms,
updated_at_ms,
parsed_at_ms,
activated_at_ms) VALUES(?,
'fbneo',
?,
?,
?,
'testdata/arcade-evidence.dat',
?,
'test',
'READY',
1,
1,
11,
0,
0,
0,
0,
0,
0,
1,
?,
?,
?,
?)
`, datID, target.ProviderID, target.TargetID, target.TargetContractSHA256,
		dummy.SHA256, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_machines(dat_version_id,
machine_name,
description,
year,
manufacturer,
cloneof,
romof,
is_explicit_bios,
classification) VALUES(?,
'evidence',
'Evidence',
'',
'',
NULL,
NULL,
0,
'NORMAL')
`, datID); err != nil {
		t.Fatal(err)
	}
	archiveFiles := make(map[string][]byte, 11)
	for index := 0; index < 10; index++ {
		archiveFiles[fmt.Sprintf("rom-%02d.bin", index)] = bytes.Repeat([]byte{byte('a' + index)}, index+1)
	}
	archiveFiles["duplicate.bin"] = append([]byte(nil), archiveFiles["rom-09.bin"]...)
	archiveBytes := makeDeterministicZIP(t, archiveFiles)
	archiveMetadata, err := blobs.Put(bytes.NewReader(archiveBytes))
	testassert.False(t, err != nil, err)
	entries, err := scanZIPForTest(blobs.Path(archiveMetadata.SHA256))
	testassert.False(t, err != nil, err)
	for ordinal, entry := range entries {
		if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_rom_entries(dat_version_id,
machine_name,
ordinal,
name,
size_bytes,
crc32,
sha1,
status) VALUES(?,
'evidence',
?,
?,
?,
?,
?,
'GOOD')
`, datID, ordinal, entry.Name, entry.Size, entry.CRC32, entry.SHA1); err != nil {
			t.Fatal(err)
		}
	}
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	upload, err := uploadService.Create(
		ctx,
		uploads.CreateRequest{
			SourceType: "FILES",
			Files: []uploads.FileDeclaration{
				{ClientFileID: "arcade", RelativePath: "evidence.zip", SizeBytes: int64(len(archiveBytes))},
			},
		},
	)
	testassert.False(t, err != nil, err)
	digest := sha256.Sum256(archiveBytes)
	digestHeader := "sha-256=:" + base64.StdEncoding.EncodeToString(digest[:]) + ":"
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0, fmt.Sprintf("bytes 0-%d/%d", len(archiveBytes)-1, len(archiveBytes)), digestHeader, bytes.NewReader(archiveBytes)); err != nil {
		t.Fatal(err)
	}
	current, _ := uploadService.Get(ctx, upload.ID)
	finalizeJob, _, err := uploadService.Complete(ctx, upload.ID, current.Version)
	testassert.False(t, err != nil, err)
	waitForState(t, database.SQL.QueryRowContext, `
SELECT state
FROM jobs
WHERE id=?
`, finalizeJob, "SUCCEEDED")
	var requestBodies [][]byte
	var requestLock sync.Mutex
	client := doerFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/v1/Lookup/ByHash" {
			pngBytes, decodeErr := base64.StdEncoding.DecodeString(
				"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
			)
			if decodeErr != nil {
				return nil, decodeErr
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"image/png"}},
				Body:       io.NopCloser(bytes.NewReader(pngBytes)),
			}, nil
		}
		contents, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			return nil, readErr
		}
		requestLock.Lock()
		requestBodies = append(requestBodies, contents)
		requestLock.Unlock()
		return httpResponse(
			http.StatusOK,
			"application/json",
			`{"id":50192,"name":"Shared Arcade Candidate","platform":{"name":"Arcade"},"signature":{"game":{"year":"1990"}},"attributes":[{"attributeName":"Logo","attributeType":"ImageId","attributeRelationType":"None","value":"arcade-logo","link":"/api/v1/images/arcade-logo"}]}`,
		), nil
	})
	resolver := resolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	})
	scraper := metadatascrape.New(database.SQL, blobs, hasheous.New(client, resolver, time.Now), time.Now)
	importer := libraryimport.New(database.SQL, time.Now, scraper).WithBlobStore(blobs)
	created, err := importer.Create(
		ctx,
		libraryimport.CreateRequest{
			UploadID:                 upload.ID,
			TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "arcade/fbneo"),
			MetadataProvider:         "NONE",
		},
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return created.ItemCount != 1 }), "create arcade import = %#v, error=%v", created, err)
	var itemID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id
FROM import_items
WHERE import_job_id=?
`, created.ImportJobID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	scheduled, _, err := scraper.ScheduleReview(ctx, itemID, 1, "HASHEOUS")
	testassert.False(t, err != nil, err)
	waitForState(t, database.SQL.QueryRowContext, `
SELECT state
FROM jobs
WHERE id=?
`, scheduled.JobID, "SUCCEEDED")
	var evidenceCount, arcadeProfileCount, leakedHashCount int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT count(*),
sum(profile='ARCADE_DAT_ENTRIES'),
sum(md5 IS NOT NULL
OR sha256 IS NOT NULL)
FROM content_hash_evidence
WHERE scrape_run_id=?
`, scheduled.RunID).Scan(&evidenceCount, &arcadeProfileCount, &leakedHashCount); err != nil {
		t.Fatal(err)
	}
	requestLock.Lock()
	bodies := append([][]byte(nil), requestBodies...)
	requestLock.Unlock()
	testassert.Falsef(t, testassert.Any(func() bool { return evidenceCount != 8 }, func() bool { return arcadeProfileCount != 8 }, func() bool { return leakedHashCount != 0 }, func() bool { return len(bodies) != 8 }), "arcade evidence/profile/leaked/requests = %d/%d/%d/%d", evidenceCount, arcadeProfileCount, leakedHashCount, len(bodies))
	for _, body := range bodies {
		var values map[string]string
		if err := json.Unmarshal(body, &values); err != nil || len(values) != 2 || values["crc"] == "" ||
			values["shA1"] == "" {
			t.Fatalf("arcade lookup body = %s, error=%v", body, err)
		}
	}
	var candidateCount, hitCount, readyAssetCount int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM scrape_candidates WHERE scrape_run_id=?),
(SELECT count(*)
FROM scrape_candidate_hits h
JOIN scrape_candidates c ON c.id=h.scrape_candidate_id
WHERE c.scrape_run_id=?),
(SELECT count(*)
FROM scrape_candidate_assets a
JOIN scrape_candidates c ON c.id=a.scrape_candidate_id
WHERE c.scrape_run_id=? AND a.status='READY')
`, scheduled.RunID, scheduled.RunID, scheduled.RunID).Scan(&candidateCount, &hitCount, &readyAssetCount); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return candidateCount != 1 }, func() bool { return hitCount != 8 }, func() bool { return readyAssetCount != 1 }), "aggregated arcade candidate/hits/ready assets = %d/%d/%d", candidateCount, hitCount, readyAssetCount)
}

type testArchiveEntry struct {
	Name, CRC32, SHA1 string
	Size              int64
}

func scanZIPForTest(path string) ([]testArchiveEntry, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer func() { cleanup.Error("close", reader.Close()) }()
	result := make([]testArchiveEntry, 0, len(reader.File))
	for _, file := range reader.File {
		source, err := file.Open()
		if err != nil {
			return nil, err
		}
		contents, readErr := io.ReadAll(source)
		cleanup.Error("close", source.Close())
		if readErr != nil {
			return nil, readErr
		}
		result = append(
			result,
			testArchiveEntry{
				Name:  file.Name,
				Size:  int64(len(contents)),
				CRC32: fmt.Sprintf("%08x", file.CRC32),
				SHA1:  fmt.Sprintf("%x", sha1Digest(contents)),
			},
		)
	}
	return result, nil
}

func sha1Digest(contents []byte) []byte {
	_, digest := legacychecksum.Sum(contents)
	decoded, _ := hex.DecodeString(digest)
	return decoded
}

func makeDeterministicZIP(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		header.SetMode(0o644)
		header.Modified = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)
		part, err := writer.CreateHeader(header)
		testassert.False(t, err != nil, err)
		if _, err := part.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

type queryRow func(context.Context, string, ...any) *sql.Row

func waitForState(t *testing.T, query queryRow, statement string, id string, expected string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var state string
		if err := query(context.Background(), statement, id).Scan(&state); err == nil && state == expected {
			return
		}
		testassert.Falsef(t, time.Now().After(deadline), "state did not become %s", expected)
		time.Sleep(10 * time.Millisecond)
	}
}

func httpResponse(status int, mediaType, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{mediaType}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
