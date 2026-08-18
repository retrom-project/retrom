//go:build integration

package libraryimport

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
	"retrom/internal/dependencies"
	"retrom/internal/importing"
	"retrom/internal/launch"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/store"
	"retrom/internal/tagging"
	"retrom/internal/uploads"
)

func TestMain(m *testing.M) {
	handled, err := importing.RunArchiveWorker(os.Args[1:])
	if handled {
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestSevenZipImportMaterializesSingleROMAndPreservesEvidence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	archiveBytes, err := os.ReadFile(filepath.Join(repositoryRoot, "internal", "importing", "testdata", "sevenzip", "single.7z"))
	if err != nil {
		t.Fatal(err)
	}
	payloadBytes, err := os.ReadFile(filepath.Join(repositoryRoot, "internal", "importing", "testdata", "sevenzip", "payload", "game.a26"))
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	upload, err := uploadService.Create(ctx, uploads.CreateRequest{
		SourceType: "FILES",
		Files: []uploads.FileDeclaration{{
			ClientFileID: "archive", RelativePath: "fixture.7z", SizeBytes: int64(len(archiveBytes)),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	archiveDigest := sha256.Sum256(archiveBytes)
	if err := uploadService.PutPart(
		ctx,
		upload.ID,
		upload.Files[0].ID,
		0,
		fmt.Sprintf("bytes 0-%d/%d", len(archiveBytes)-1, len(archiveBytes)),
		"sha-256=:"+base64.StdEncoding.EncodeToString(archiveDigest[:])+":",
		bytes.NewReader(archiveBytes),
	); err != nil {
		t.Fatal(err)
	}
	current, err := uploadService.Get(ctx, upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _, err := uploadService.Complete(ctx, upload.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(3 * time.Second); ; {
		var state string
		if err := database.SQL.QueryRowContext(ctx, "SELECT state FROM jobs WHERE id=?", jobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("upload finalization = %s", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	created, err := New(database.SQL, time.Now).WithBlobStore(blobs).Create(ctx, CreateRequest{
		UploadID: upload.ID, TargetPlatformInstanceID: "01980000-0000-7000-8000-000000000011", MetadataProvider: "NONE",
	})
	if err != nil || created.ItemCount != 1 {
		t.Fatalf("Create() = %#v, error=%v", created, err)
	}
	var itemID, sourceArchiveBlobID, contentBlobID, logicalName, archiveFormat, compressionProfile, contentSHA string
	var sourceOrdinal int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT i.id,
       source.source_archive_blob_id,
       source.source_archive_entry_ordinal,
       source.blob_id,
       source.logical_name,
       entry.archive_format,
       entry.compression_profile,
       content.sha256
FROM import_items i
JOIN import_item_source_files source ON source.import_item_id=i.id AND source.role='CONTENT'
JOIN archive_entries entry ON entry.archive_blob_id=source.source_archive_blob_id
 AND entry.ordinal=source.source_archive_entry_ordinal
JOIN blobs content ON content.id=source.blob_id
WHERE i.import_job_id=?
`, created.ImportJobID).Scan(
		&itemID,
		&sourceArchiveBlobID,
		&sourceOrdinal,
		&contentBlobID,
		&logicalName,
		&archiveFormat,
		&compressionProfile,
		&contentSHA,
	); err != nil {
		t.Fatal(err)
	}
	payloadDigest := sha256.Sum256(payloadBytes)
	if sourceArchiveBlobID == "" || contentBlobID == sourceArchiveBlobID || sourceOrdinal != 0 ||
		logicalName != "game.a26" || archiveFormat != "SEVEN_Z" ||
		compressionProfile != "SEVEN_Z_DECODER_VALIDATED" || contentSHA != hex.EncodeToString(payloadDigest[:]) {
		t.Fatalf(
			"materialized source = archive:%s ordinal:%d content:%s name:%s format:%s/%s sha:%s",
			sourceArchiveBlobID,
			sourceOrdinal,
			contentBlobID,
			logicalName,
			archiveFormat,
			compressionProfile,
			contentSHA,
		)
	}
	approved, err := New(database.SQL, time.Now).Approve(ctx, itemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	var publishedBlobID, publishedArchiveID string
	var publishedOrdinal int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT file.blob_id,file.source_archive_blob_id,file.source_archive_entry_ordinal
FROM games game
JOIN game_content_files file ON file.game_content_revision_id=game.current_content_revision_id
WHERE game.id=? AND file.role='CONTENT'
`, approved.GameID).Scan(&publishedBlobID, &publishedArchiveID, &publishedOrdinal); err != nil {
		t.Fatal(err)
	}
	if publishedBlobID != contentBlobID || publishedArchiveID != sourceArchiveBlobID || publishedOrdinal != 0 {
		t.Fatalf("published source = %s/%s/%d", publishedBlobID, publishedArchiveID, publishedOrdinal)
	}
}

func TestUploadImportReviewPublishPipeline(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	const (
		profileID = "01980000-0000-7000-8000-00000000a461"
		adminID   = "01980000-0000-7000-8000-00000000b461"
	)
	if _, err := database.SQL.ExecContext(ctx,
		`INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,'Import Tag Admin',1)`, profileID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,?,'import.tag.admin','Import Tag Admin','ADMIN','ENABLED',1,1)
`, adminID, profileID); err != nil {
		t.Fatal(err)
	}
	ctx = authn.WithPrincipal(ctx, authn.Principal{UserID: adminID, ProfileID: profileID, Role: "ADMIN"})
	defaultTag, err := tagging.New(database.SQL, time.Now).Create(ctx, adminID, "待通关")
	if err != nil {
		t.Fatal(err)
	}
	blobs, _ := blobstore.Open(dataDir)
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	contents := []byte("deterministic gba fixture")
	upload, err := uploadService.Create(
		ctx,
		uploads.CreateRequest{
			SourceType: "FILES",
			Files: []uploads.FileDeclaration{
				{ClientFileID: "game", RelativePath: "Sudoku.gba", SizeBytes: int64(len(contents))},
				{ClientFileID: "discard", RelativePath: "Discarded.gba", SizeBytes: int64(len(contents))},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	digestHeader := "sha-256=:" + base64.StdEncoding.EncodeToString(digest[:]) + ":"
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0, fmt.Sprintf("bytes 0-%d/%d", len(contents)-2, len(contents)), digestHeader, bytes.NewReader(contents)); err == nil {
		t.Fatal("range/body length mismatch succeeded")
	}
	rangeHeader := "bytes 0-24/25"
	if len(contents) != 25 {
		t.Fatalf("fixture size changed: %d", len(contents))
	}
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0, rangeHeader, digestHeader, bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[1].ID, 0, rangeHeader, digestHeader, bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	current, _ := uploadService.Get(ctx, upload.ID)
	jobID, _, err := uploadService.Complete(ctx, upload.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var state string
		_ = database.SQL.QueryRowContext(ctx, "SELECT state FROM jobs WHERE id=?", jobID).Scan(&state)
		if state == "SUCCEEDED" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("upload finalization = %s", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	created, err := New(
		database.SQL,
		time.Now,
	).WithBlobStore(blobs).
		Create(ctx, CreateRequest{UploadID: upload.ID, TargetPlatformInstanceID: "01980000-0000-7000-8000-000000000005", MetadataProvider: "NONE", TagIDs: []string{defaultTag.TagID}})
	if err != nil {
		t.Fatalf("create import: %v", err)
	}
	var itemID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT i.id
FROM import_items i
JOIN import_item_source_files f ON f.import_item_id=i.id
WHERE i.import_job_id=?
AND f.logical_name='Sudoku.gba'
`, created.ImportJobID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	var discardItemID, discardBlobID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT i.id,
f.blob_id
FROM import_items i
JOIN import_item_source_files f ON f.import_item_id=i.id
WHERE i.import_job_id=?
AND f.logical_name='Discarded.gba'
	`, created.ImportJobID).Scan(&discardItemID, &discardBlobID); err != nil {
		t.Fatal(err)
	}
	var inheritedDrafts int
	var initialConfigSnapshot string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT count(DISTINCT draft.id),job.config_snapshot_json
FROM import_jobs job
JOIN import_items item ON item.import_job_id=job.id
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN review_draft_tags relation ON relation.review_draft_id=draft.id AND relation.tag_id=?
WHERE job.id=?
`, defaultTag.TagID, created.ImportJobID).Scan(&inheritedDrafts, &initialConfigSnapshot); err != nil ||
		inheritedDrafts != 2 || !strings.Contains(initialConfigSnapshot, `"name":"待通关"`) {
		t.Fatalf("default tag inheritance = drafts:%d config:%s error:%v", inheritedDrafts, initialConfigSnapshot, err)
	}
	importer := New(database.SQL, time.Now)
	transientTag, err := tagging.New(database.SQL, time.Now).Create(ctx, adminID, "删除失效")
	if err != nil {
		t.Fatal(err)
	}
	transientDraft, err := importer.PatchDraft(ctx, discardItemID, 1, DraftPatch{
		TagIDs: []string{defaultTag.TagID, transientTag.TagID},
	})
	if err != nil || transientDraft.Version != 2 {
		t.Fatalf("add transient review tag = %#v, %v", transientDraft, err)
	}
	currentTransientTag, err := tagging.New(database.SQL, time.Now).Get(ctx, transientTag.TagID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tagging.New(database.SQL, time.Now).Delete(
		ctx, adminID, transientTag.TagID, transientTag.Name, currentTransientTag.Version,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := importer.PatchDraft(ctx, discardItemID, 2, DraftPatch{TagIDs: []string{defaultTag.TagID}}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale review after tag delete error = %v", err)
	}
	refreshedTransientDraft, err := importer.PatchDraft(
		ctx, discardItemID, 3, DraftPatch{TagIDs: []string{defaultTag.TagID}},
	)
	if err != nil || refreshedTransientDraft.Version != 4 || len(refreshedTransientDraft.Tags) != 1 ||
		refreshedTransientDraft.Tags[0].TagID != defaultTag.TagID {
		t.Fatalf("refresh deleted review tag = %#v, %v", refreshedTransientDraft, err)
	}
	var metadataPatch DraftPatch
	if err := json.Unmarshal([]byte(`{"metadata":{"players":2,"releaseYear":2001},"tagIds":[]}`), &metadataPatch); err != nil {
		t.Fatal(err)
	}
	patched, err := importer.PatchDraft(ctx, itemID, 1, metadataPatch)
	if err != nil || patched.Version != 2 || patched.Metadata["players"] != int64(2) ||
		patched.Metadata["releaseYear"] != int64(2001) {
		t.Fatalf("patch nullable metadata values = %#v, error=%v", patched, err)
	}
	if err := json.Unmarshal([]byte(`{"metadata":{"players":null,"releaseYear":null},"tagIds":[]}`), &metadataPatch); err != nil {
		t.Fatal(err)
	}
	patched, err = importer.PatchDraft(ctx, itemID, 2, metadataPatch)
	if err != nil || patched.Version != 3 || patched.Metadata["players"] != nil ||
		patched.Metadata["releaseYear"] != nil {
		t.Fatalf("clear nullable metadata values = %#v, error=%v", patched, err)
	}
	crossPlatform := "01980000-0000-7000-8000-000000000001"
	_, err = importer.PatchDraft(ctx, itemID, 3, DraftPatch{TargetPlatformInstanceID: &crossPlatform, TagIDs: []string{}})
	if !errors.Is(err, ErrReimportRequiredPlatformChange) {
		t.Fatalf("cross-platform draft change error = %v", err)
	}
	var oldValidationID, importConfigSnapshot string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT d.selected_validation_id,
j.config_snapshot_json
FROM review_drafts d
JOIN import_items i ON i.id=d.import_item_id
JOIN import_jobs j ON j.id=i.import_job_id
WHERE d.import_item_id=?
`, itemID).Scan(&oldValidationID, &importConfigSnapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE platform_instances
SET version=version+1,
updated_at_ms=updated_at_ms+1
WHERE id='01980000-0000-7000-8000-000000000005'
`); err != nil {
		t.Fatal(err)
	}
	if _, err := importer.Approve(ctx, itemID, 3); !errors.Is(err, ErrInvalid) {
		t.Fatalf("stale selected validation approval error = %v", err)
	}
	if err := json.Unmarshal([]byte(`{"metadata":{"title":"Sudoku"},"tagIds":[]}`), &metadataPatch); err != nil {
		t.Fatal(err)
	}
	refreshed, err := importer.PatchDraft(ctx, itemID, 3, metadataPatch)
	if err != nil || refreshed.Version != 4 {
		t.Fatalf("refresh config validation = %#v, error=%v", refreshed, err)
	}
	var refreshedValidationID string
	var refreshedPlatformVersion int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT d.selected_validation_id,
v.platform_instance_version
FROM review_drafts d
JOIN import_item_core_validations v ON v.id=d.selected_validation_id
WHERE d.import_item_id=?
	`, itemID).Scan(&refreshedValidationID, &refreshedPlatformVersion); err != nil ||
		refreshedValidationID == oldValidationID || refreshedPlatformVersion != 2 ||
		!strings.Contains(importConfigSnapshot, `"platformInstanceVersion":1`) {
		t.Fatalf("old/new validation snapshot = %s/%s v%d config=%s error=%v", oldValidationID, refreshedValidationID, refreshedPlatformVersion, importConfigSnapshot, err)
	}
	var sourceBlobID string
	if err := database.SQL.QueryRowContext(ctx, "SELECT final_blob_id FROM upload_files WHERE id=?", upload.Files[0].ID).Scan(&sourceBlobID); err != nil {
		t.Fatal(err)
	}
	var requirementID, md5Value, sha1Value, sha256Value string
	var requirementVersion, sourceSize int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id,version
FROM bios_requirements
WHERE core_id='mgba' AND logical_name='gba_bios.bin' AND enabled=1
`).Scan(&requirementID, &requirementVersion); err != nil {
		t.Fatal(err)
	}
	if err := database.SQL.QueryRowContext(ctx, `
SELECT size_bytes,md5,sha1,sha256
FROM blobs
WHERE id=?
`, sourceBlobID).Scan(&sourceSize, &md5Value, &sha1Value, &sha256Value); err != nil {
		t.Fatal(err)
	}
	const biosInstallationID = "01990000-0000-7000-8000-000000000010"
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO bios_installations(id,requirement_id,blob_id,original_filename,size_bytes,md5,sha1,sha256,
validated_requirement_version,status,validation_details_json,is_active,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,'HASH_WARNING','{}',1,1,?,?)
`, biosInstallationID, requirementID, sourceBlobID, "gba_bios.bin", sourceSize, md5Value, sha1Value,
		sha256Value, requirementVersion, time.Now().UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"metadata":{"description":"BIOS snapshot refreshed"},"tagIds":[]}`), &metadataPatch); err != nil {
		t.Fatal(err)
	}
	biosRefreshed, err := importer.PatchDraft(ctx, itemID, 4, metadataPatch)
	if err != nil || biosRefreshed.Version != 5 {
		t.Fatalf("refresh BIOS validation = %#v, error=%v", biosRefreshed, err)
	}
	var biosValidationID, biosSnapshotJSON, validationBIOSBlobID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT d.selected_validation_id,v.dependency_snapshot_json,f.blob_id
FROM review_drafts d
JOIN import_item_core_validations v ON v.id=d.selected_validation_id
JOIN import_item_validation_files f ON f.import_item_core_validation_id=v.id
AND f.role='BIOS_BUNDLE' AND f.logical_name='gba_bios.bin'
WHERE d.import_item_id=?
`, itemID).Scan(&biosValidationID, &biosSnapshotJSON, &validationBIOSBlobID); err != nil {
		t.Fatal(err)
	}
	biosSnapshot, err := corevalidation.ParseSnapshot(biosSnapshotJSON)
	if err != nil || biosValidationID == refreshedValidationID || validationBIOSBlobID != sourceBlobID ||
		len(biosSnapshot.BIOS) != 1 || biosSnapshot.BIOS[0].InstallationID == nil ||
		*biosSnapshot.BIOS[0].InstallationID != biosInstallationID {
		t.Fatalf("refreshed BIOS validation = %s snapshot=%s blob=%s error=%v", biosValidationID, biosSnapshotJSON, validationBIOSBlobID, err)
	}
	manualCoverID := "01990000-0000-7000-8000-000000000001"
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO review_uploaded_assets(id,import_item_id,upload_file_id,blob_id,kind,width_px,height_px,media_type,created_at_ms)
VALUES(?,?,?,?,'COVER',600,900,'image/png',?)
`, manualCoverID, itemID, upload.Files[0].ID, sourceBlobID, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	selected, err := importer.PatchDraft(ctx, itemID, 5, DraftPatch{
		SelectedAssets: &SelectedAssets{CoverUploadedAssetID: &manualCoverID}, TagIDs: []string{defaultTag.TagID},
	})
	if err != nil || selected.Version != 6 {
		t.Fatalf("select uploaded review cover = %#v, error=%v", selected, err)
	}
	approved, err := importer.Approve(ctx, itemID, 6)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	var title, variantStatus, publishedCoverBlobID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT m.title,
r.status,
(SELECT blob_id FROM game_assets WHERE game_id=g.id AND metadata_revision_id=g.current_metadata_revision_id AND kind='COVER')
FROM games g
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN game_variants v ON v.game_id=g.id
JOIN game_variant_revisions r ON r.id=v.current_revision_id
WHERE g.id=?
`, approved.GameID).Scan(&title, &variantStatus, &publishedCoverBlobID); err != nil {
		t.Fatal(err)
	}
	if title != "Sudoku" || variantStatus != "READY" || publishedCoverBlobID != sourceBlobID {
		t.Fatalf("published title/status/cover = %s/%s/%s", title, variantStatus, publishedCoverBlobID)
	}
	var publishedTags int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT count(*) FROM game_tags WHERE game_id=? AND tag_id=?
`, approved.GameID, defaultTag.TagID).Scan(&publishedTags); err != nil || publishedTags != 1 {
		t.Fatalf("published game tag = %d, %v", publishedTags, err)
	}
	discarded, err := importer.Discard(ctx, discardItemID, refreshedTransientDraft.Version, "")
	if err != nil || discarded.Status != "DISCARDED" {
		t.Fatalf("discard review = %#v, error=%v", discarded, err)
	}
	var discardedJobState, discardedItemState string
	var discardedJobPending, discardedJobPublished, discardedJobDiscarded int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT job.state,
job.review_pending_item_count,
job.published_item_count,
job.discarded_item_count,
item.state
FROM import_jobs job
JOIN import_items item ON item.import_job_id=job.id
WHERE job.id=? AND item.id=?
`, created.ImportJobID, discardItemID).Scan(
		&discardedJobState,
		&discardedJobPending,
		&discardedJobPublished,
		&discardedJobDiscarded,
		&discardedItemState,
	); err != nil {
		t.Fatal(err)
	}
	if discardedJobState != "COMPLETED" || discardedJobPending != 0 || discardedJobPublished != 1 ||
		discardedJobDiscarded != 1 || discardedItemState != "DISCARDED" {
		t.Fatalf(
			"discard aggregate = job:%s pending:%d published:%d discarded:%d item:%s",
			discardedJobState,
			discardedJobPending,
			discardedJobPublished,
			discardedJobDiscarded,
			discardedItemState,
		)
	}
	var publishedDiscard, retainedBlob int
	var beforeJSON, configEvidenceJSON, datEvidenceJSON string
	var discardReason sql.NullString
	if err := database.SQL.QueryRowContext(ctx, `
SELECT (SELECT count(*)
FROM games g
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
WHERE m.title='Discarded'),
(SELECT count(*) FROM blobs WHERE id=?),
before_json,
config_evidence_json,
dat_evidence_json
,
reason
FROM review_events
WHERE id=?
`, discardBlobID, discarded.EventID).Scan(&publishedDiscard, &retainedBlob, &beforeJSON, &configEvidenceJSON, &datEvidenceJSON, &discardReason); err != nil ||
		publishedDiscard != 0 || retainedBlob != 1 ||
		discardReason.Valid ||
		!strings.Contains(beforeJSON, "sourceManifest") ||
		!strings.Contains(beforeJSON, `"name":"待通关"`) ||
		!strings.Contains(configEvidenceJSON, "configSnapshot") ||
		!strings.Contains(datEvidenceJSON, "dependencySnapshot") {
		t.Fatalf("discard evidence = games:%d blob:%d before:%s config:%s dat:%s error=%v", publishedDiscard, retainedBlob, beforeJSON, configEvidenceJSON, datEvidenceJSON, err)
	}
	if _, err := database.SQL.ExecContext(ctx, `UPDATE review_events SET reason='tampered' WHERE id=?`, discarded.EventID); err == nil ||
		!strings.Contains(err.Error(), "immutable") {
		t.Fatalf("immutable review event update error = %v", err)
	}
}

func TestRetryAndCancelKeepImportItemAggregatesInSync(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	var artifactID string
	if err := database.SQL.QueryRow(`
SELECT id FROM core_artifacts WHERE core_id='mgba' AND enabled=1
`).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	const (
		retryUploadID  = "01980000-0000-7000-8000-00000000a171"
		retryImportID  = "01980000-0000-7000-8000-00000000b171"
		retryItemID    = "01980000-0000-7000-8000-00000000c171"
		cancelUploadID = "01980000-0000-7000-8000-00000000a172"
		cancelImportID = "01980000-0000-7000-8000-00000000b172"
	)
	digest := strings.Repeat("a", 64)
	for _, uploadID := range []string{retryUploadID, cancelUploadID} {
		if _, err := database.SQL.Exec(`
INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,version,
expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'COMPLETE','FILES',1,0,?,1,10000,1000,1000)
`, uploadID, digest); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.SQL.Exec(`
INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,
platform_id,default_core_id,core_artifact_id,metadata_provider,config_snapshot_json,config_snapshot_digest,
state,total_item_count,failed_item_count,version,created_at_ms,updated_at_ms)
VALUES(?,?,'01980000-0000-7000-8000-000000000005',1,'gba','mgba',?,'NONE','{}',?,
'PARTIAL_FAILURE',1,1,1,1000,1000)
`, retryImportID, retryUploadID, artifactID, digest); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.Exec(`
INSERT INTO import_items(id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,
search_text,failed_stage,last_error_code,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,'FAILED_RETRYABLE','{}',?,'retry.gba','SCRAPING','PROVIDER_TIMEOUT',1,1000,1000)
`, retryItemID, retryImportID, strings.Repeat("b", 64), digest); err != nil {
		t.Fatal(err)
	}
	service := New(database.SQL, func() time.Time { return time.UnixMilli(2_000) })
	retried, err := service.RetryItem(ctx, retryItemID, 1)
	if err != nil || retried.State != "QUEUED" || retried.Version != 2 {
		t.Fatalf("retry = %#v, error=%v", retried, err)
	}
	var retryJobState, retryItemState string
	var retryQueued, retryFailed, retryJobVersion, retryEventCount int64
	if err := database.SQL.QueryRow(`
SELECT job.state,job.queued_item_count,job.failed_item_count,job.version,item.state,
(SELECT count(*) FROM job_events WHERE job_id=? AND event_type='MANUAL_RETRY')
FROM import_jobs job
JOIN import_items item ON item.import_job_id=job.id
WHERE job.id=? AND item.id=?
`, retried.JobID, retryImportID, retryItemID).Scan(
		&retryJobState,
		&retryQueued,
		&retryFailed,
		&retryJobVersion,
		&retryItemState,
		&retryEventCount,
	); err != nil {
		t.Fatal(err)
	}
	if retryJobState != "RUNNING" || retryQueued != 1 || retryFailed != 0 || retryJobVersion != 2 ||
		retryItemState != "QUEUED" || retryEventCount != 1 {
		t.Fatalf(
			"retry aggregate = job:%s queued:%d failed:%d version:%d item:%s events:%d",
			retryJobState,
			retryQueued,
			retryFailed,
			retryJobVersion,
			retryItemState,
			retryEventCount,
		)
	}

	if _, err := database.SQL.Exec(`
INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,
platform_id,default_core_id,core_artifact_id,metadata_provider,config_snapshot_json,config_snapshot_digest,
state,total_item_count,queued_item_count,review_pending_item_count,failed_item_count,version,created_at_ms,updated_at_ms)
VALUES(?,?,'01980000-0000-7000-8000-000000000005',1,'gba','mgba',?,'NONE','{}',?,
'PARTIAL_FAILURE',4,1,1,2,1,1000,1000)
`, cancelImportID, cancelUploadID, artifactID, digest); err != nil {
		t.Fatal(err)
	}
	cancelItems := []struct {
		id        string
		state     string
		stage     any
		errorCode any
	}{
		{"01980000-0000-7000-8000-00000000c172", "QUEUED", nil, nil},
		{"01980000-0000-7000-8000-00000000c173", "REVIEW_PENDING", nil, nil},
		{"01980000-0000-7000-8000-00000000c174", "FAILED_RETRYABLE", "SCRAPING", "PROVIDER_TIMEOUT"},
		{"01980000-0000-7000-8000-00000000c175", "FAILED_FINAL", "IDENTIFYING", "UNSUPPORTED_CONTENT"},
	}
	for index, item := range cancelItems {
		if _, err := database.SQL.Exec(`
INSERT INTO import_items(id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,
search_text,failed_stage,last_error_code,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,'{}',?,'cancel.gba',?,?,1,1000,1000)
`, item.id, cancelImportID, strings.Repeat(fmt.Sprintf("%x", index+1), 64), item.state, digest,
			item.stage, item.errorCode); err != nil {
			t.Fatal(err)
		}
	}
	cancelled, pending, err := service.Cancel(ctx, cancelImportID, 1, "operator cancelled")
	if err != nil || pending || cancelled.State != "CANCELLED" || cancelled.Version != 2 {
		t.Fatalf("cancel = %#v pending=%t error=%v", cancelled, pending, err)
	}
	var cancelState string
	var cancelQueued, cancelReviewPending, cancelFailed, cancelCount, cancelCompletedAt int64
	var cancelledItems, failedFinalItems int64
	if err := database.SQL.QueryRow(`
SELECT state,queued_item_count,review_pending_item_count,failed_item_count,cancelled_item_count,completed_at_ms,
(SELECT count(*) FROM import_items WHERE import_job_id=import_jobs.id AND state='CANCELLED'),
(SELECT count(*) FROM import_items WHERE import_job_id=import_jobs.id AND state='FAILED_FINAL')
FROM import_jobs WHERE id=?
`, cancelImportID).Scan(
		&cancelState,
		&cancelQueued,
		&cancelReviewPending,
		&cancelFailed,
		&cancelCount,
		&cancelCompletedAt,
		&cancelledItems,
		&failedFinalItems,
	); err != nil {
		t.Fatal(err)
	}
	if cancelState != "CANCELLED" || cancelQueued != 0 || cancelReviewPending != 0 || cancelFailed != 1 ||
		cancelCount != 3 || cancelCompletedAt != 2_000 || cancelledItems != 3 || failedFinalItems != 1 {
		t.Fatalf(
			"cancel aggregate = job:%s queued:%d pending:%d failed:%d cancelled:%d completed:%d items:%d/%d",
			cancelState,
			cancelQueued,
			cancelReviewPending,
			cancelFailed,
			cancelCount,
			cancelCompletedAt,
			cancelledItems,
			failedFinalItems,
		)
	}
}

func TestDuplicateContentIsSkippedDuringIdentificationAndConfirmedDuringReview(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	uploader := uploads.New(database.SQL, blobs, dataDir, time.Now)
	importer := New(database.SQL, time.Now).WithBlobStore(blobs)
	contents := []byte("duplicate-content-identity-fixture")
	const platformInstanceID = "01980000-0000-7000-8000-000000000005"

	createImport := func(name string) (Created, string) {
		t.Helper()
		upload, createErr := uploader.Create(ctx, uploads.CreateRequest{
			SourceType: "FILES",
			Files: []uploads.FileDeclaration{{
				ClientFileID: "game", RelativePath: name, SizeBytes: int64(len(contents)),
			}},
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		digest := sha256.Sum256(contents)
		if putErr := uploader.PutPart(
			ctx,
			upload.ID,
			upload.Files[0].ID,
			0,
			fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)),
			"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":",
			bytes.NewReader(contents),
		); putErr != nil {
			t.Fatal(putErr)
		}
		current, getErr := uploader.Get(ctx, upload.ID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		jobID, _, completeErr := uploader.Complete(ctx, upload.ID, current.Version)
		if completeErr != nil {
			t.Fatal(completeErr)
		}
		waitForJob(t, database, jobID)
		created, importErr := importer.Create(ctx, CreateRequest{
			UploadID: upload.ID, TargetPlatformInstanceID: platformInstanceID, MetadataProvider: "NONE",
		})
		if importErr != nil {
			t.Fatal(importErr)
		}
		var itemID string
		if queryErr := database.SQL.QueryRowContext(ctx, `
SELECT id FROM import_items WHERE import_job_id=?
`, created.ImportJobID).Scan(&itemID); queryErr != nil {
			t.Fatal(queryErr)
		}
		return created, itemID
	}

	firstImport, firstItemID := createImport("first-name.gba")
	secondImport, secondItemID := createImport("renamed-copy.gba")
	if firstImport.State != "REVIEW_PENDING" || secondImport.State != "REVIEW_PENDING" {
		t.Fatalf("pre-publish import states = %s/%s", firstImport.State, secondImport.State)
	}
	firstGame, err := importer.Approve(ctx, firstItemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	duplicates, identityDigest, err := importer.DuplicateGames(ctx, secondItemID)
	if err != nil || len(duplicates) != 1 || duplicates[0].GameID != firstGame.GameID || len(identityDigest) != 64 {
		t.Fatalf("review duplicates = %#v digest=%s error=%v", duplicates, identityDigest, err)
	}
	if _, err := importer.Approve(ctx, secondItemID, 1); err == nil {
		t.Fatal("duplicate review published without confirmation")
	} else {
		var conflict *DuplicateConflict
		if !errors.As(err, &conflict) || !errors.Is(err, ErrDuplicateContent) ||
			len(conflict.Games) != 1 || conflict.Games[0].GameID != firstGame.GameID {
			t.Fatalf("duplicate approval error = %#v", err)
		}
	}
	secondGame, err := importer.ApproveWithDecision(ctx, secondItemID, 1, ApprovalDecision{
		DuplicatePolicy: "ALLOW_NEW", AcknowledgedGameIDs: []string{firstGame.GameID},
	})
	if err != nil {
		t.Fatal(err)
	}
	var auditDiff string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT diff_json FROM review_events WHERE id=?
`, secondGame.EventID).Scan(&auditDiff); err != nil ||
		!strings.Contains(auditDiff, `"duplicatePolicy":"ALLOW_NEW"`) ||
		!strings.Contains(auditDiff, firstGame.GameID) ||
		!strings.Contains(auditDiff, identityDigest) {
		t.Fatalf("duplicate audit diff = %s, error=%v", auditDiff, err)
	}

	thirdImport, thirdItemID := createImport("another-wrapper-name.gba")
	if thirdImport.State != "COMPLETED" {
		t.Fatalf("identification duplicate state = %s", thirdImport.State)
	}
	var jobState, itemState string
	var alreadyItems, alreadyFiles, discarded, pending, draftCount, matchCount, gameCount int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT state,already_imported_item_count,already_imported_file_count,
discarded_item_count,review_pending_item_count
FROM import_jobs WHERE id=?
`, thirdImport.ImportJobID).Scan(
		&jobState, &alreadyItems, &alreadyFiles, &discarded, &pending,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.SQL.QueryRowContext(ctx, `
SELECT state,
(SELECT count(*) FROM review_drafts WHERE import_item_id=import_items.id),
(SELECT count(*) FROM import_item_duplicate_matches WHERE import_item_id=import_items.id)
FROM import_items WHERE id=?
`, thirdItemID).Scan(&itemState, &draftCount, &matchCount); err != nil {
		t.Fatal(err)
	}
	if err := database.SQL.QueryRowContext(ctx, `SELECT count(*) FROM games WHERE status='PUBLISHED'`).Scan(&gameCount); err != nil {
		t.Fatal(err)
	}
	if jobState != "COMPLETED" || itemState != "DISCARDED" || alreadyItems != 1 || alreadyFiles != 1 ||
		discarded != 1 || pending != 0 || draftCount != 0 || matchCount != 2 || gameCount != 2 {
		t.Fatalf(
			"identification projection = job:%s item:%s already:%d/%d discarded:%d pending:%d drafts:%d matches:%d games:%d",
			jobState, itemState, alreadyItems, alreadyFiles, discarded, pending, draftCount, matchCount, gameCount,
		)
	}
}

func TestImportGroupsSingleArchiveMemberAndReportsEveryFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	rom := []byte("raw-gba-member")
	archive := makeZIP(t, map[string][]byte{"folder/Wrapped.gba": rom, "README.txt": []byte("readme")})
	files := map[string][]byte{
		"Wrapped.zip":        archive,
		"wrong-platform.iso": []byte("raw-psp-content"),
		".DS_Store":          []byte("sidecar"),
	}
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	declarations := make([]uploads.FileDeclaration, 0, len(files))
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		contents := files[path]
		declarations = append(
			declarations,
			uploads.FileDeclaration{ClientFileID: path, RelativePath: path, SizeBytes: int64(len(contents))},
		)
	}
	upload, err := uploadService.Create(ctx, uploads.CreateRequest{SourceType: "FILES", Files: declarations})
	if err != nil {
		t.Fatal(err)
	}
	fileByPath := make(map[string]uploads.File, len(upload.Files))
	for _, file := range upload.Files {
		fileByPath[file.RelativePath] = file
	}
	for _, path := range paths {
		contents := files[path]
		digest := sha256.Sum256(contents)
		header := "sha-256=:" + base64.StdEncoding.EncodeToString(digest[:]) + ":"
		if err := uploadService.PutPart(ctx, upload.ID, fileByPath[path].ID, 0, fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)), header, bytes.NewReader(contents)); err != nil {
			t.Fatal(err)
		}
	}
	current, err := uploadService.Get(ctx, upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _, err := uploadService.Complete(ctx, upload.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, database, jobID)
	importer := New(database.SQL, time.Now).WithBlobStore(blobs)
	created, err := importer.Create(
		ctx,
		CreateRequest{
			UploadID:                 upload.ID,
			TargetPlatformInstanceID: "01980000-0000-7000-8000-000000000005",
			MetadataProvider:         "NONE",
		},
	)
	if err != nil || created.State != "PARTIAL_FAILURE" || created.ItemCount != 1 {
		t.Fatalf("archive import = %#v, error=%v", created, err)
	}
	var source, ignored, rejected, itemCount int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT
(SELECT count(*)
FROM import_job_files
WHERE import_job_id=?
AND disposition='SOURCE'),
(SELECT count(*)
FROM import_job_files
WHERE import_job_id=?
AND disposition='IGNORED'
AND reason_code='IGNORED_SYSTEM_SIDECAR'),
(SELECT count(*)
FROM import_job_files
WHERE import_job_id=?
AND disposition='REJECTED'
AND reason_code='UNSUPPORTED_CONTENT_FORMAT'),
(SELECT count(*)
FROM import_items
WHERE import_job_id=?)
`, created.ImportJobID, created.ImportJobID, created.ImportJobID, created.ImportJobID).Scan(
		&source,
		&ignored,
		&rejected,
		&itemCount,
	); err != nil || source != 1 || ignored != 1 || rejected != 1 || itemCount != 1 {
		t.Fatalf("file dispositions/items = %d/%d/%d/%d, error=%v", source, ignored, rejected, itemCount, err)
	}
	var itemID, logicalName, contentSHA, archiveSHA string
	var archiveOrdinal int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT i.id,
s.logical_name,
b.sha256,
archive.sha256,
s.source_archive_entry_ordinal
FROM import_items i
JOIN import_item_source_files s ON s.import_item_id=i.id
JOIN blobs b ON b.id=s.blob_id
JOIN blobs archive ON archive.id=s.source_archive_blob_id
WHERE i.import_job_id=?
`, created.ImportJobID).Scan(&itemID, &logicalName, &contentSHA, &archiveSHA, &archiveOrdinal); err != nil {
		t.Fatal(err)
	}
	romDigest := sha256.Sum256(rom)
	archiveDigest := sha256.Sum256(archive)
	if logicalName != "Wrapped.gba" || contentSHA != fmt.Sprintf("%x", romDigest) ||
		archiveSHA != fmt.Sprintf("%x", archiveDigest) ||
		archiveOrdinal < 0 {
		t.Fatalf("archive source = %s %s %s %d", logicalName, contentSHA, archiveSHA, archiveOrdinal)
	}
	approved, err := importer.Approve(ctx, itemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	var publishedLogical, publishedSHA string
	var sourceArchive string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT f.logical_name,
b.sha256,
f.source_archive_blob_id
FROM games g
JOIN game_content_files f ON f.game_content_revision_id=g.current_content_revision_id
JOIN blobs b ON b.id=f.blob_id
WHERE g.id=?
`, approved.GameID).Scan(&publishedLogical, &publishedSHA, &sourceArchive); err != nil ||
		publishedLogical != "Wrapped.gba" ||
		publishedSHA != fmt.Sprintf("%x", romDigest) ||
		sourceArchive == "" {
		t.Fatalf("published archive member = %s/%s/%s, error=%v", publishedLogical, publishedSHA, sourceArchive, err)
	}
	var finalState string
	var completedAt sql.NullInt64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT state,
completed_at_ms
FROM import_jobs
WHERE id=?
`, created.ImportJobID).Scan(&finalState, &completedAt); err != nil ||
		finalState != "PARTIAL_FAILURE" ||
		completedAt.Valid {
		t.Fatalf("import with rejected evidence finalized as %s/%v, error=%v", finalState, completedAt, err)
	}
	var sourceVersion int64
	if err := database.SQL.QueryRowContext(ctx, `SELECT version FROM import_jobs WHERE id=?`, created.ImportJobID).Scan(&sourceVersion); err != nil {
		t.Fatal(err)
	}
	reconfigured, err := importer.Reconfigure(ctx, created.ImportJobID, sourceVersion, ReconfigureRequest{
		TargetPlatformInstanceID: "01980000-0000-7000-8000-000000000023",
		MetadataProvider:         "NONE",
	})
	if err != nil || reconfigured.State != "REVIEW_PENDING" || reconfigured.ItemCount != 1 {
		t.Fatalf("reconfigured import = %#v, error=%v", reconfigured, err)
	}
	var sourceState, replacementSource, replacementLogicalName, sourceBlobSHA, replacementBlobSHA string
	var resolvedRejected int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT source.state,
source.resolved_rejected_file_count,
replacement.reconfigured_from_import_job_id,
replacement_file.logical_name,
source_blob.sha256,
replacement_blob.sha256
FROM import_jobs source
JOIN import_jobs replacement ON replacement.id=?
JOIN import_items replacement_item ON replacement_item.import_job_id=replacement.id
JOIN import_item_source_files replacement_file ON replacement_file.import_item_id=replacement_item.id
JOIN blobs replacement_blob ON replacement_blob.id=replacement_file.blob_id
JOIN import_job_file_resolutions resolution ON resolution.import_job_id=source.id
AND resolution.replacement_import_job_id=replacement.id
JOIN upload_files source_file ON source_file.id=resolution.upload_file_id
JOIN blobs source_blob ON source_blob.id=source_file.final_blob_id
WHERE source.id=?
`, reconfigured.ImportJobID, created.ImportJobID).Scan(
		&sourceState,
		&resolvedRejected,
		&replacementSource,
		&replacementLogicalName,
		&sourceBlobSHA,
		&replacementBlobSHA,
	); err != nil || sourceState != "COMPLETED" || resolvedRejected != 1 ||
		replacementSource != created.ImportJobID || replacementLogicalName != "wrong-platform.iso" ||
		sourceBlobSHA != replacementBlobSHA {
		t.Fatalf(
			"reconfiguration evidence = %s/%d/%s/%s/%s/%s, error=%v",
			sourceState,
			resolvedRejected,
			replacementSource,
			replacementLogicalName,
			sourceBlobSHA,
			replacementBlobSHA,
			err,
		)
	}
	if _, err := importer.Reconfigure(ctx, created.ImportJobID, sourceVersion, ReconfigureRequest{
		TargetPlatformInstanceID: "01980000-0000-7000-8000-000000000023",
		MetadataProvider:         "NONE",
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("stale reconfiguration error = %v", err)
	}
}

func TestDOSDirectoryGroupingProducesDeterministicBundleAndSafePrograms(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	inputs := []struct {
		path     string
		contents []byte
	}{
		{"GAME/INSTALL.BAT", []byte("install")},
		{"GAME/DOOM.EXE", []byte("exe")},
		{"GAME/DATA.WAD", []byte("wad")},
		{"BAD%NAME.BAT", []byte("bat")},
	}
	files := make([]importSourceFile, 0, len(inputs))
	for index, input := range inputs {
		metadata, err := blobs.Put(bytes.NewReader(input.contents))
		if err != nil {
			t.Fatal(err)
		}
		files = append(
			files,
			importSourceFile{
				id:     fmt.Sprintf("file-%d", index),
				path:   input.path,
				blobID: fmt.Sprintf("blob-%d", index),
				sha256: metadata.SHA256,
			},
		)
	}
	service := (&Service{}).WithBlobStore(blobs)
	dispositions, groups, _ := service.prepareDOSFiles(context.Background(), "DIRECTORY", files)
	if len(dispositions) != 4 || len(groups) != 1 || len(groups[0].sources) != 4 || len(groups[0].dosEntries) != 3 ||
		groups[0].defaultDOSEntry != "GAME/DOOM.EXE" ||
		groups[0].bundle == nil {
		t.Fatalf("DOS grouping = dispositions:%d groups:%#v", len(dispositions), groups)
	}
	if !groups[0].dosEntries[0].safe || groups[0].dosEntries[1].safe ||
		groups[0].dosEntries[2].path != "GAME/INSTALL.BAT" || groups[0].dosEntries[2].rank != 2 {
		t.Fatalf("DOS direct safety = %#v", groups[0].dosEntries)
	}
	_, repeated, _ := service.prepareDOSFiles(context.Background(), "DIRECTORY", files)
	if len(repeated) != 1 || repeated[0].bundle == nil || repeated[0].bundle.SHA256 != groups[0].bundle.SHA256 {
		t.Fatalf("DOS bundle hash drift = %#v / %#v", groups[0].bundle, repeated)
	}
	multi, noGroups, _ := service.prepareDOSFiles(context.Background(), "FILES", files[:2])
	if len(noGroups) != 0 || len(multi) != 2 || multi[0].reason != "AMBIGUOUS_DOS_BUNDLE" ||
		multi[1].reason != "AMBIGUOUS_DOS_BUNDLE" {
		t.Fatalf("ambiguous DOS files = %#v / %#v", multi, noGroups)
	}
	zipBytes := makeZIP(t, map[string][]byte{"GAME/DOOM.EXE": []byte("exe"), "GAME/DATA.WAD": []byte("wad")})
	zipMetadata, err := blobs.Put(bytes.NewReader(zipBytes))
	if err != nil {
		t.Fatal(err)
	}
	zipFile := importSourceFile{id: "zip-file", path: "Doom.zip", blobID: "zip-blob", sha256: zipMetadata.SHA256}
	zipDispositions, zipGroups, archives := service.prepareDOSFiles(
		context.Background(),
		"FILES",
		[]importSourceFile{zipFile},
	)
	if len(zipDispositions) != 1 || len(zipGroups) != 1 || len(zipGroups[0].sources) != 2 || len(archives) != 1 ||
		len(archives[0].materialized) != 2 {
		t.Fatalf("DOS ZIP grouping = dispositions:%#v groups:%#v archives:%#v", zipDispositions, zipGroups, archives)
	}
	for _, source := range zipGroups[0].sources {
		if source.role != "DOS_SOURCE" || source.archiveOrdinal == nil || source.archiveBlobID != "zip-blob" {
			t.Fatalf("DOS ZIP source = %#v", source)
		}
	}
	for _, unsafe := range []string{"GAME/100%.BAT", "GAME/QUOTE\".EXE", "GAME/TRAILING .EXE ", "GAME/TRAILING.EXE.", "游戏.EXE"} {
		if directDOSPathSafe(unsafe) {
			t.Fatalf("unsafe DOS path accepted: %q", unsafe)
		}
	}
}

func TestArcadeDraftBIOSStateRefreshesInstalledDATMachineDependency(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	var artifactID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id FROM core_artifacts WHERE core_id='fbneo' AND enabled=1
`).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	archive := makeZIP(t, map[string][]byte{"b.bin": []byte("bios")})
	metadata, err := blobs.Put(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	blobID, err := blobstore.EnsureRecord(ctx, database.SQL, metadata, "application/zip", time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	const requirementID = "01990000-0000-7000-8000-000000000101"
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO bios_requirements(id,core_id,core_artifact_id,source_kind,dat_machine_name,logical_name,
requirement_mode,condition_code,activation_options_json,catalog_digest,size_bytes,md5,sha1,sha256,
source_url,source_version,enabled,version,created_at_ms,updated_at_ms,delivery_kind,emulator_path)
VALUES(?,'fbneo',?,'DAT_MACHINE','bios','bios.zip','REQUIRED','ARCADE_DAT_DEPENDENCY','{}',?,
NULL,NULL,NULL,NULL,'test://bios','test',1,1,?,?,'BIOS_BUNDLE',NULL)
`, requirementID, artifactID, strings.Repeat("a", 64), time.Now().UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO bios_installations(id,requirement_id,blob_id,original_filename,size_bytes,md5,sha1,sha256,
validated_requirement_version,status,validation_details_json,is_active,version,created_at_ms,updated_at_ms)
VALUES('01990000-0000-7000-8000-000000000102',?,?,?, ?,?,?,?,1,'MATCHED','{}',1,1,?,?)
`, requirementID, blobID, "bios.zip", metadata.Size, metadata.MD5, metadata.SHA1, metadata.SHA256,
		time.Now().UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	previous := `{"schemaVersion":1,"machine":"child","datVersionId":"dat-test","closure":["child","bios"],"dependencies":[{"kind":"BIOS_OR_BASE","machine":"bios","state":"MISSING","requiredEntries":["b.bin"]}],"missingEntries":["bios.zip"],"mismatchedEntries":[],"warnings":[]}`
	transaction, err := database.SQL.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Rollback(transaction) })
	resolved, err := resolveArcadeDraftBIOSState(
		ctx, transaction, artifactID, previous, "BLOCKED", "LAUNCH_BIOS_MISSING",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.tracked || resolved.replaceBundle || resolved.status != "READY" || resolved.code != "READY" ||
		len(resolved.dependencies) != 1 || resolved.dependencies[0].BlobID == nil || *resolved.dependencies[0].BlobID != blobID {
		t.Fatalf("resolved arcade BIOS state = %#v", resolved)
	}
	var snapshot arcadeDraftSnapshot
	if err := json.Unmarshal([]byte(resolved.snapshotJSON), &snapshot); err != nil || len(snapshot.MissingEntries) != 0 ||
		len(snapshot.Dependencies) != 1 || snapshot.Dependencies[0].State != "SATISFIED_EXTERNAL" {
		t.Fatalf("resolved arcade snapshot = %#v, error=%v", snapshot, err)
	}
}

func TestArcadeImportUsesInstalledBIOSBeforeCreatingReview(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	var artifactID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id FROM core_artifacts WHERE core_id='fbneo' AND enabled=1
`).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	dummy, err := blobs.Put(bytes.NewReader([]byte("synthetic DAT")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE dat_versions SET is_active=0,version=version+1,updated_at_ms=?
WHERE core_artifact_id=? AND is_active=1
`, now, artifactID); err != nil {
		t.Fatal(err)
	}
	const datID = "01990000-0000-7000-8000-000000000201"
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_versions(id,core_id,core_artifact_id,source,builtin_relative_path,sha256,parser_version,
compatibility_status,parse_status,is_active,machine_count,rom_entry_count,disk_entry_count,
bios_set_count,default_bios_set_count,explicit_bios_machine_count,base_dependency_target_count,
unresolved_relation_count,version,created_at_ms,updated_at_ms,parsed_at_ms,activated_at_ms)
VALUES(?,'fbneo',?,'BUILTIN','testdata/installed-bios.dat',?,'test','MATCHED','READY',1,2,2,0,0,0,1,1,0,1,?,?,?,?)
`, datID, artifactID, dummy.SHA256, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_machines(dat_version_id,machine_name,description,year,manufacturer,cloneof,romof,
is_explicit_bios,classification) VALUES
(?,'codexchild','Child','','',NULL,'codexbios',0,'NORMAL'),
(?,'codexbios','BIOS','','',NULL,NULL,1,'EXPLICIT_BIOS')
`, datID, datID); err != nil {
		t.Fatal(err)
	}
	childArchive := makeZIP(t, map[string][]byte{"c.bin": []byte("child")})
	biosArchive := makeZIP(t, map[string][]byte{"b.bin": []byte("bios")})
	type archiveRecord struct {
		machine string
		entry   string
		body    []byte
		bytes   []byte
	}
	for _, fixture := range []archiveRecord{
		{machine: "codexchild", entry: "c.bin", body: []byte("child"), bytes: childArchive},
		{machine: "codexbios", entry: "b.bin", body: []byte("bios"), bytes: biosArchive},
	} {
		metadata, putErr := blobs.Put(bytes.NewReader(fixture.bytes))
		if putErr != nil {
			t.Fatal(putErr)
		}
		entries, scanErr := importing.ScanZIP(ctx, blobs.Path(metadata.SHA256), importing.DefaultArchiveLimits())
		if scanErr != nil || len(entries) != 1 {
			t.Fatalf("scan %s = %#v, error=%v", fixture.machine, entries, scanErr)
		}
		if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_rom_entries(dat_version_id,machine_name,ordinal,name,size_bytes,crc32,sha1,status)
VALUES(?,?,0,?,?,?,?,'GOOD')
`, datID, fixture.machine, fixture.entry, len(fixture.body), entries[0].CRC32, entries[0].SHA1); err != nil {
			t.Fatal(err)
		}
	}
	biosMetadata, err := blobs.Put(bytes.NewReader(biosArchive))
	if err != nil {
		t.Fatal(err)
	}
	biosBlobID, err := blobstore.EnsureRecord(ctx, database.SQL, biosMetadata, "application/zip", now)
	if err != nil {
		t.Fatal(err)
	}
	const requirementID = "01990000-0000-7000-8000-000000000202"
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO bios_requirements(id,core_id,core_artifact_id,source_kind,dat_machine_name,logical_name,
requirement_mode,condition_code,activation_options_json,catalog_digest,size_bytes,md5,sha1,sha256,
source_url,source_version,enabled,version,created_at_ms,updated_at_ms,delivery_kind,emulator_path)
VALUES(?,'fbneo',?,'DAT_MACHINE','codexbios','codexbios.zip','REQUIRED',
'ARCADE_DAT_DEPENDENCY','{}',?,NULL,NULL,NULL,NULL,'test://bios',?,1,1,?,?,'BIOS_BUNDLE',NULL)
`, requirementID, artifactID, strings.Repeat("a", 64), datID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO bios_installations(id,requirement_id,blob_id,original_filename,size_bytes,md5,sha1,sha256,
validated_requirement_version,status,validation_details_json,is_active,version,created_at_ms,updated_at_ms)
VALUES('01990000-0000-7000-8000-000000000203',?,?,?, ?,?,?,?,1,'MATCHED','{}',1,1,?,?)
`, requirementID, biosBlobID, "codexbios.zip", biosMetadata.Size, biosMetadata.MD5, biosMetadata.SHA1,
		biosMetadata.SHA256, now, now); err != nil {
		t.Fatal(err)
	}
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	upload, err := uploadService.Create(ctx, uploads.CreateRequest{
		SourceType: "FILES",
		Files: []uploads.FileDeclaration{{
			ClientFileID: "child", RelativePath: "codexchild.zip", SizeBytes: int64(len(childArchive)),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(childArchive)
	if err := uploadService.PutPart(
		ctx,
		upload.ID,
		upload.Files[0].ID,
		0,
		fmt.Sprintf("bytes 0-%d/%d", len(childArchive)-1, len(childArchive)),
		"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":",
		bytes.NewReader(childArchive),
	); err != nil {
		t.Fatal(err)
	}
	current, err := uploadService.Get(ctx, upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _, err := uploadService.Complete(ctx, upload.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var state string
		if err := database.SQL.QueryRowContext(ctx, "SELECT state FROM jobs WHERE id=?", jobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("upload finalization = %s", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	importService := New(database.SQL, time.Now).WithBlobStore(blobs)
	created, err := importService.Create(ctx, CreateRequest{
		UploadID: upload.ID, TargetPlatformInstanceID: "01980000-0000-7000-8000-000000000006",
		MetadataProvider: "NONE",
	})
	if err != nil {
		t.Fatal(err)
	}
	var validationID, status, code, snapshotJSON string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT draft.selected_validation_id,validation.status,validation.compatibility_code,
validation.dependency_snapshot_json
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_core_validations validation ON validation.id=draft.selected_validation_id
WHERE item.import_job_id=?
`, created.ImportJobID).Scan(&validationID, &status, &code, &snapshotJSON); err != nil {
		t.Fatal(err)
	}
	if status != "READY" || code != "READY" {
		t.Fatalf("initial validation = %s/%s", status, code)
	}
	var snapshot arcadeDraftSnapshot
	if err := json.Unmarshal([]byte(snapshotJSON), &snapshot); err != nil || len(snapshot.MissingEntries) != 0 ||
		len(snapshot.Dependencies) != 1 || snapshot.Dependencies[0].State != "SATISFIED_EXTERNAL" {
		t.Fatalf("initial snapshot = %#v, error=%v", snapshot, err)
	}
	var validationBlobID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT blob_id FROM import_item_validation_files
WHERE import_item_core_validation_id=? AND role='BIOS_BUNDLE' AND logical_name='codexbios.zip'
`, validationID).Scan(&validationBlobID); err != nil {
		t.Fatal(err)
	}
	if validationBlobID != biosBlobID {
		t.Fatalf("initial BIOS blob = %s, want %s", validationBlobID, biosBlobID)
	}
	var itemID string
	var draftVersion int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT item.id,draft.version
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
WHERE item.import_job_id=?
	`, created.ImportJobID).Scan(&itemID, &draftVersion); err != nil {
		t.Fatal(err)
	}
	approved, err := importService.Approve(ctx, itemID, draftVersion)
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce the immutable legacy production shape: older Arcade approvals
	// could leave their current revision on a schema-v1 runtime snapshot even
	// though the revision remained locked to the built-in DAT.
	const legacyRevisionID = "01990000-0000-7000-8000-000000000205"
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO game_variant_revisions(id,game_variant_id,game_content_revision_id,core_artifact_id,dat_version_id,
validation_input_digest,emulator_game_id,status,compatibility_code,dependency_snapshot_json,default_dos_entry,created_at_ms)
SELECT ?,revision.game_variant_id,revision.game_content_revision_id,revision.core_artifact_id,revision.dat_version_id,
?,revision.emulator_game_id+1,revision.status,revision.compatibility_code,'{"schemaVersion":1,"bios":[]}',
revision.default_dos_entry,?
FROM game_variant_revisions revision
JOIN game_variants variant ON variant.current_revision_id=revision.id
WHERE variant.game_id=? AND variant.core_id='fbneo'
`, legacyRevisionID, strings.Repeat("f", 64), now, approved.GameID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE game_variants SET current_revision_id=?,version=version+1,updated_at_ms=?
WHERE game_id=? AND core_id='fbneo'
`, legacyRevisionID, now, approved.GameID); err != nil {
		t.Fatal(err)
	}
	replacementMetadata, err := blobs.Put(bytes.NewReader(append(biosArchive, []byte("replacement")...)))
	if err != nil {
		t.Fatal(err)
	}
	replacementBlobID, err := blobstore.EnsureRecord(
		ctx, database.SQL, replacementMetadata, "application/zip", now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE bios_installations SET is_active=0,version=version+1,updated_at_ms=?
WHERE requirement_id=? AND is_active=1
`, now+1, requirementID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO bios_installations(id,requirement_id,blob_id,original_filename,size_bytes,md5,sha1,sha256,
validated_requirement_version,status,validation_details_json,is_active,version,created_at_ms,updated_at_ms)
VALUES('01990000-0000-7000-8000-000000000204',?,?,?, ?,?,?,?,1,'HASH_WARNING','{}',1,1,?,?)
`, requirementID, replacementBlobID, "codexbios.zip", replacementMetadata.Size, replacementMetadata.MD5,
		replacementMetadata.SHA1, replacementMetadata.SHA256, now+1, now+1); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('local','Arcade BIOS Fixture',?)
`, now); err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	launcher := launch.New(database.SQL, dependencySet, credentials, time.Now)
	coreID := "fbneo"
	capabilities := launch.Capabilities{
		SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true,
	}
	legacyEntries, err := json.Marshal(snapshot.Dependencies[0].RequiredEntries)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO variant_dependencies(game_variant_revision_id,kind,logical_archive,dat_version_id,
source_machine_name,required_entries_json,state,created_at_ms)
VALUES(?,'BIOS_OR_BASE','codexbios.zip',?,'codexbios',?,'SATISFIED_EXTERNAL',?)
`, legacyRevisionID, datID, string(legacyEntries), now); err != nil {
		t.Fatal(err)
	}
	pending, err := launcher.Create(ctx, "local", launch.CreateRequest{
		GameID: approved.GameID, CoreID: &coreID, ReturnTo: "/games/" + approved.GameID,
		ClientCapabilities: capabilities,
	})
	if err != nil || pending.Status != "VALIDATION_PENDING" || pending.JobID == "" {
		t.Fatalf("legacy Arcade validation = %#v, error=%v", pending, err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		var state string
		var errorCode sql.NullString
		if err := database.SQL.QueryRowContext(ctx, `SELECT state,error_code FROM jobs WHERE id=?`, pending.JobID).
			Scan(&state, &errorCode); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" {
			break
		}
		if state == "FAILED" || time.Now().After(deadline) {
			t.Fatalf("legacy Arcade BIOS validation = %s/%s", state, errorCode.String)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var migratedSnapshot string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT revision.dependency_snapshot_json
FROM game_variants variant
JOIN game_variant_revisions revision ON revision.id=variant.current_revision_id
WHERE variant.game_id=? AND variant.core_id='fbneo'
`, approved.GameID).Scan(&migratedSnapshot); err != nil {
		t.Fatal(err)
	}
	var migratedEnvelope struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal([]byte(migratedSnapshot), &migratedEnvelope); err != nil || migratedEnvelope.SchemaVersion != 2 {
		t.Fatalf("migrated Arcade dependency snapshot = %s, error=%v", migratedSnapshot, err)
	}
	createdLaunch, err := launcher.Create(ctx, "local", launch.CreateRequest{
		GameID: approved.GameID, CoreID: &coreID, ReturnTo: "/games/" + approved.GameID,
		ClientCapabilities: capabilities,
	})
	if err != nil || createdLaunch.LaunchID == "" {
		t.Fatalf("Arcade BIOS launch after revalidation = %#v, error=%v", createdLaunch, err)
	}
	configuration, err := launcher.Config(ctx, createdLaunch.LaunchID, createdLaunch.Capability)
	if err != nil || configuration.BIOSURL == nil {
		t.Fatalf("Arcade BIOS launch config = %#v, error=%v", configuration, err)
	}
	bundle, err := launcher.BundleFiles(ctx, createdLaunch.LaunchID, createdLaunch.Capability, "BIOS_BUNDLE")
	if err != nil || len(bundle) != 1 || bundle[0].LogicalName != "codexbios.zip" ||
		bundle[0].SHA256 != replacementMetadata.SHA256 {
		t.Fatalf("Arcade BIOS launch bundle = %#v, error=%v", bundle, err)
	}
}

func TestArcadeGroupingBuildsCoreScopedParentAndBIOSClosure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	dummy, err := blobs.Put(bytes.NewReader([]byte("synthetic dat")))
	if err != nil {
		t.Fatal(err)
	}
	var artifactID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id
FROM core_artifacts
WHERE core_id='fbneo'
AND enabled=1
`).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	datID := "01980000-0000-7000-8000-000000000201"
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_versions(id,
core_id,
core_artifact_id,
source,
builtin_relative_path,
sha256,
parser_version,
compatibility_status,
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
parsed_at_ms) VALUES(?,
'fbneo',
?,
'BUILTIN',
'testdata/grouping.dat',
?,
'test',
'MATCHED',
'READY',
0,
3,
3,
0,
0,
0,
1,
1,
0,
1,
?,
?,
?)
`,
		datID,
		artifactID,
		dummy.SHA256,
		time.Now().UnixMilli(),
		time.Now().UnixMilli(),
		time.Now().UnixMilli(),
	); err != nil {
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
classification) VALUES
(?,
'child',
'Child',
'',
'',
'parent',
'bios',
0,
'NORMAL'),
(?,
'parent',
'Parent',
'',
'',
NULL,
'bios',
0,
'NORMAL'),
(?,
'bios',
'BIOS',
'',
'',
NULL,
NULL,
1,
'EXPLICIT_BIOS')
`, datID, datID, datID); err != nil {
		t.Fatal(err)
	}
	type archiveFixture struct {
		id, name, entry string
		body            []byte
		file            importSourceFile
	}
	fixtures := []archiveFixture{
		{id: "child-file", name: "child.zip", entry: "c.bin", body: []byte("child")},
		{id: "parent-file", name: "parent.zip", entry: "p.bin", body: []byte("parent")},
		{id: "bios-file", name: "bios.zip", entry: "b.bin", body: []byte("bios")},
	}
	for index := range fixtures {
		archiveBytes := makeZIP(t, map[string][]byte{fixtures[index].entry: fixtures[index].body})
		metadata, putErr := blobs.Put(bytes.NewReader(archiveBytes))
		if putErr != nil {
			t.Fatal(putErr)
		}
		fixtures[index].file = importSourceFile{
			id:     fixtures[index].id,
			path:   fixtures[index].name,
			blobID: "blob-" + fixtures[index].id,
			sha256: metadata.SHA256,
		}
		entryDigest := sha256.Sum256(fixtures[index].body)
		entries, scanErr := importing.ScanZIP(ctx, blobs.Path(metadata.SHA256), importing.DefaultArchiveLimits())
		if scanErr != nil || len(entries) != 1 {
			t.Fatalf("scan %s = %#v, error=%v", fixtures[index].name, entries, scanErr)
		}
		machine := strings.TrimSuffix(fixtures[index].name, ".zip")
		if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_rom_entries(dat_version_id,
machine_name,
ordinal,
name,
size_bytes,
crc32,
sha1,
status) VALUES(?,
?,
?,
?,
?,
?,
?,
'GOOD')
`,
			datID,
			machine,
			0,
			fixtures[index].entry,
			len(fixtures[index].body),
			entries[0].CRC32,
			entries[0].SHA1,
		); err != nil {
			t.Fatalf("insert ROM %x: %v", entryDigest, err)
		}
	}
	service := (&Service{database: database.SQL}).WithBlobStore(blobs)
	files := []importSourceFile{fixtures[0].file, fixtures[1].file, fixtures[2].file}
	dispositions, groups, archives := service.prepareArcadeFiles(ctx, files, sql.NullString{String: datID, Valid: true})
	if len(dispositions) != 3 || len(groups) != 1 || len(archives) != 3 {
		t.Fatalf(
			"arcade grouping counts = dispositions:%#v groups:%#v archives:%d",
			dispositions,
			groups,
			len(archives),
		)
	}
	child := groups[0]
	if child.validationStatus != "READY" || len(child.sources) != 3 || len(child.validationFiles) != 2 ||
		child.validationFiles[0].role != "PARENT" ||
		child.validationFiles[1].role != "BIOS_BUNDLE" {
		t.Fatalf("child dependency closure = %#v", child)
	}
	for _, disposition := range dispositions {
		if disposition.disposition != "SOURCE" {
			t.Fatalf("referenced archive disposition = %#v", disposition)
		}
	}
	missingDispositions, missingGroups, _ := service.prepareArcadeFiles(
		ctx,
		[]importSourceFile{fixtures[0].file},
		sql.NullString{String: datID, Valid: true},
	)
	if len(missingDispositions) != 1 || len(missingGroups) != 1 || missingGroups[0].validationStatus != "BLOCKED" ||
		missingGroups[0].compatibilityCode != "LAUNCH_BIOS_MISSING" &&
			missingGroups[0].compatibilityCode != "LAUNCH_PARENT_MISSING" {
		t.Fatalf("missing dependency = dispositions:%#v groups:%#v", missingDispositions, missingGroups)
	}
	fullBytes := makeZIP(
		t,
		map[string][]byte{"c.bin": []byte("child"), "p.bin": []byte("parent"), "b.bin": []byte("bios")},
	)
	fullMetadata, err := blobs.Put(bytes.NewReader(fullBytes))
	if err != nil {
		t.Fatal(err)
	}
	_, fullGroups, _ := service.prepareArcadeFiles(
		ctx,
		[]importSourceFile{{id: "full", path: "child.zip", blobID: "full-blob", sha256: fullMetadata.SHA256}},
		sql.NullString{String: datID, Valid: true},
	)
	if len(fullGroups) != 1 || fullGroups[0].validationStatus != "READY" || len(fullGroups[0].sources) != 1 ||
		len(fullGroups[0].validationFiles) != 0 {
		t.Fatalf("full non-merged closure = %#v", fullGroups)
	}
	fullWithCloneExtraBytes := makeZIP(
		t,
		map[string][]byte{
			"c.bin":       []byte("child"),
			"p.bin":       []byte("parent"),
			"b.bin":       []byte("bios"),
			"clone/c.bin": []byte("clone-specific alternate"),
		},
	)
	fullWithCloneExtraMetadata, err := blobs.Put(bytes.NewReader(fullWithCloneExtraBytes))
	if err != nil {
		t.Fatal(err)
	}
	_, fullWithCloneExtraGroups, _ := service.prepareArcadeFiles(
		ctx,
		[]importSourceFile{{
			id: "full-with-clone-extra", path: "child.zip", blobID: "full-with-clone-extra-blob",
			sha256: fullWithCloneExtraMetadata.SHA256,
		}},
		sql.NullString{String: datID, Valid: true},
	)
	if len(fullWithCloneExtraGroups) != 1 || fullWithCloneExtraGroups[0].validationStatus != "READY" ||
		fullWithCloneExtraGroups[0].compatibilityCode != "READY" {
		t.Fatalf("full non-merged closure with clone extra = %#v", fullWithCloneExtraGroups)
	}
	mergedBytes := makeZIP(t, map[string][]byte{"c.bin": []byte("child"), "parent/p.bin": []byte("parent")})
	mergedMetadata, err := blobs.Put(bytes.NewReader(mergedBytes))
	if err != nil {
		t.Fatal(err)
	}
	_, mergedGroups, _ := service.prepareArcadeFiles(
		ctx,
		[]importSourceFile{{id: "merged", path: "child.zip", blobID: "merged-blob", sha256: mergedMetadata.SHA256}},
		sql.NullString{String: datID, Valid: true},
	)
	if len(mergedGroups) != 1 || mergedGroups[0].validationStatus != "BLOCKED" ||
		mergedGroups[0].compatibilityCode != "UNSUPPORTED_MERGED_ROMSET" {
		t.Fatalf("merged ROM set = %#v", mergedGroups)
	}
	nestedMismatchBytes := makeZIP(
		t,
		map[string][]byte{"c.bin": []byte("child"), "parent/p.bin": []byte("not-the-parent-rom")},
	)
	nestedMismatchMetadata, err := blobs.Put(bytes.NewReader(nestedMismatchBytes))
	if err != nil {
		t.Fatal(err)
	}
	_, nestedMismatchGroups, _ := service.prepareArcadeFiles(
		ctx,
		[]importSourceFile{{
			id: "nested-mismatch", path: "child.zip", blobID: "nested-mismatch-blob",
			sha256: nestedMismatchMetadata.SHA256,
		}},
		sql.NullString{String: datID, Valid: true},
	)
	if len(nestedMismatchGroups) != 1 || nestedMismatchGroups[0].validationStatus != "BLOCKED" ||
		nestedMismatchGroups[0].compatibilityCode != "LAUNCH_PARENT_MISSING" {
		t.Fatalf("nested name-only match must remain a missing parent = %#v", nestedMismatchGroups)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_disk_entries(dat_version_id,
machine_name,
ordinal,
name,
sha1,
status) VALUES(?,
'child',
0,
'disk',
?,
'GOOD')
`, datID, strings.Repeat("0", 40)); err != nil {
		t.Fatal(err)
	}
	_, diskGroups, _ := service.prepareArcadeFiles(ctx, files, sql.NullString{String: datID, Valid: true})
	if len(diskGroups) == 0 || diskGroups[0].validationStatus != "INCOMPATIBLE" ||
		diskGroups[0].compatibilityCode != "UNSUPPORTED_CHD" {
		t.Fatalf("CHD compatibility = %#v", diskGroups)
	}
}

func makeZIP(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var contents bytes.Buffer
	writer := zip.NewWriter(&contents)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		header.SetMode(0o600)
		header.Modified = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)
		part, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return contents.Bytes()
}

func waitForJob(t *testing.T, database *store.DB, jobID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var state string
		if err := database.SQL.QueryRowContext(context.Background(), `
SELECT state
FROM jobs
WHERE id=?
`, jobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" {
			return
		}
		if state == "FAILED" || time.Now().After(deadline) {
			t.Fatalf("job %s state = %s", jobID, state)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
