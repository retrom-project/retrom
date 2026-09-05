//go:build integration

package libraryimport

import (
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
	"strings"
	"testing"
	"time"

	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
	"retrom/internal/dependencies"
	"retrom/internal/importing"
	"retrom/internal/payloadrelease"
	"retrom/internal/tagging"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
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
	archiveBytes, err := os.ReadFile(filepath.Join(repositoryRoot, "internal", "importing", "testdata", "sevenzip", "single.7z"))
	testassert.False(t, err != nil, err)
	payloadBytes, err := os.ReadFile(filepath.Join(repositoryRoot, "internal", "importing", "testdata", "sevenzip", "payload", "game.a26"))
	testassert.False(t, err != nil, err)
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	upload, err := uploadService.Create(ctx, uploads.CreateRequest{
		SourceType: "FILES",
		Files: []uploads.FileDeclaration{{
			ClientFileID: "archive", RelativePath: "fixture.7z", SizeBytes: int64(len(archiveBytes)),
		}},
	})
	testassert.False(t, err != nil, err)
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
	testassert.False(t, err != nil, err)
	jobID, _, err := uploadService.Complete(ctx, upload.ID, current.Version)
	testassert.False(t, err != nil, err)
	for deadline := time.Now().Add(3 * time.Second); ; {
		var state string
		if err := database.SQL.QueryRowContext(ctx, "SELECT state FROM jobs WHERE id=?", jobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" {
			break
		}
		testassert.Falsef(t, time.Now().After(deadline), "upload finalization = %s", state)
		time.Sleep(10 * time.Millisecond)
	}
	created, err := New(database.SQL, time.Now).WithBlobStore(blobs).Create(ctx, CreateRequest{
		UploadID:                 upload.ID,
		TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "atari2600/stella2014"),
		MetadataProvider:         "NONE",
	})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return created.ItemCount != 1 }), "Create() = %#v, error=%v", created, err)
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
	testassert.Falsef(t, testassert.Any(func() bool { return sourceArchiveBlobID == "" }, func() bool { return contentBlobID == sourceArchiveBlobID }, func() bool { return sourceOrdinal != 0 }, func() bool { return logicalName != "game.a26" }, func() bool { return archiveFormat != "SEVEN_Z" }, func() bool { return compressionProfile != "SEVEN_Z_DECODER_VALIDATED" }, func() bool { return contentSHA != hex.EncodeToString(payloadDigest[:]) }), "materialized source = archive:%s ordinal:%d content:%s name:%s format:%s/%s sha:%s", sourceArchiveBlobID, sourceOrdinal, contentBlobID, logicalName, archiveFormat, compressionProfile, contentSHA)
	approved, err := New(database.SQL, time.Now).Approve(ctx, itemID, 1)
	testassert.False(t, err != nil, err)
	var publishedBlobID, publishedArchiveID string
	var publishedOrdinal int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT file.blob_id,file.source_archive_blob_id,file.source_archive_entry_ordinal
FROM games game
JOIN game_files file ON file.game_id=game.id
WHERE game.id=? AND file.role='CONTENT'
`, approved.GameID).Scan(&publishedBlobID, &publishedArchiveID, &publishedOrdinal); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return publishedBlobID != contentBlobID }, func() bool { return publishedArchiveID != sourceArchiveBlobID }, func() bool { return publishedOrdinal != 0 }), "published source = %s/%s/%d", publishedBlobID, publishedArchiveID, publishedOrdinal)
}

func TestUploadImportReviewPublishPipeline(t *testing.T) {
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
	testassert.False(t, err != nil, err)
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
	testassert.False(t, err != nil, err)
	digest := sha256.Sum256(contents)
	digestHeader := "sha-256=:" + base64.StdEncoding.EncodeToString(digest[:]) + ":"
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0, fmt.Sprintf("bytes 0-%d/%d", len(contents)-2, len(contents)), digestHeader, bytes.NewReader(contents)); err == nil {
		t.Fatal("range/body length mismatch succeeded")
	}
	rangeHeader := "bytes 0-24/25"
	testassert.Falsef(t, len(contents) != 25, "fixture size changed: %d", len(contents))
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0, rangeHeader, digestHeader, bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[1].ID, 0, rangeHeader, digestHeader, bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	current, _ := uploadService.Get(ctx, upload.ID)
	jobID, _, err := uploadService.Complete(ctx, upload.ID, current.Version)
	testassert.False(t, err != nil, err)
	deadline := time.Now().Add(3 * time.Second)
	for {
		var state string
		_ = database.SQL.QueryRowContext(ctx, "SELECT state FROM jobs WHERE id=?", jobID).Scan(&state)
		if state == "SUCCEEDED" {
			break
		}
		testassert.Falsef(t, time.Now().After(deadline), "upload finalization = %s", state)
		time.Sleep(10 * time.Millisecond)
	}
	created, err := New(
		database.SQL,
		time.Now,
	).WithBlobStore(blobs).
		Create(ctx, CreateRequest{UploadID: upload.ID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "gba/mgba"), MetadataProvider: "NONE", TagIDs: []string{defaultTag.TagID}})
	testassert.Falsef(t, err != nil, "create import: %v", err)
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
	testassert.False(t, err != nil, err)
	transientDraft, err := importer.PatchDraft(ctx, discardItemID, 1, DraftPatch{
		TagIDs: []string{defaultTag.TagID, transientTag.TagID},
	})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return transientDraft.Version != 2 }), "add transient review tag = %#v, %v", transientDraft, err)
	currentTransientTag, err := tagging.New(database.SQL, time.Now).Get(ctx, transientTag.TagID)
	testassert.False(t, err != nil, err)
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
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return refreshedTransientDraft.Version != 4 }, func() bool { return len(refreshedTransientDraft.Tags) != 1 }, func() bool { return refreshedTransientDraft.Tags[0].TagID != defaultTag.TagID }), "refresh deleted review tag = %#v, %v", refreshedTransientDraft, err)
	var metadataPatch DraftPatch
	if err := json.Unmarshal([]byte(`{"metadata":{"players":2,"releaseYear":2001},"tagIds":[]}`), &metadataPatch); err != nil {
		t.Fatal(err)
	}
	patched, err := importer.PatchDraft(ctx, itemID, 1, metadataPatch)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return patched.Version != 2 }, func() bool { return patched.Metadata["players"] != int64(2) }, func() bool { return patched.Metadata["releaseYear"] != int64(2001) }), "patch nullable metadata values = %#v, error=%v", patched, err)
	if err := json.Unmarshal([]byte(`{"metadata":{"players":null,"releaseYear":null},"tagIds":[]}`), &metadataPatch); err != nil {
		t.Fatal(err)
	}
	patched, err = importer.PatchDraft(ctx, itemID, 2, metadataPatch)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return patched.Version != 3 }, func() bool { return patched.Metadata["players"] != nil }, func() bool { return patched.Metadata["releaseYear"] != nil }), "clear nullable metadata values = %#v, error=%v", patched, err)
	crossPlatform := testsupport.MustPlatformInstanceID(t, database.SQL, "nes/fceumm")
	_, err = importer.PatchDraft(ctx, itemID, 3, DraftPatch{TargetPlatformInstanceID: &crossPlatform, TagIDs: []string{}})
	testassert.Truef(t, errors.Is(err, ErrReimportRequiredPlatformChange), "cross-platform draft change error = %v", err)
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
WHERE catalog_template_key='gba/mgba'
`); err != nil {
		t.Fatal(err)
	}
	if current, err := importer.ReviewValidationCurrent(ctx, oldValidationID); err != nil || !current {
		t.Fatalf("folder presentation change invalidated review: current=%v, error=%v", current, err)
	}
	if err := json.Unmarshal([]byte(`{"metadata":{"title":"Sudoku"},"tagIds":[]}`), &metadataPatch); err != nil {
		t.Fatal(err)
	}
	refreshed, err := importer.PatchDraft(ctx, itemID, 3, metadataPatch)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return refreshed.Version != 4 }), "refresh config validation = %#v, error=%v", refreshed, err)
	var refreshedValidationID string
	var refreshedPlatformVersion int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT d.selected_validation_id,
v.platform_instance_version
FROM review_drafts d
JOIN import_item_core_validations v ON v.id=d.selected_validation_id
WHERE d.import_item_id=?
	`, itemID).Scan(&refreshedValidationID, &refreshedPlatformVersion); err != nil ||
		refreshedValidationID != oldValidationID || refreshedPlatformVersion != 1 ||
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
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return biosRefreshed.Version != 5 }), "refresh BIOS validation = %#v, error=%v", biosRefreshed, err)
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
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return biosValidationID == refreshedValidationID }, func() bool { return validationBIOSBlobID != sourceBlobID }, func() bool { return len(biosSnapshot.BIOS) != 1 }, func() bool { return biosSnapshot.BIOS[0].InstallationID == nil }, func() bool { return *biosSnapshot.BIOS[0].InstallationID != biosInstallationID }), "refreshed BIOS validation = %s snapshot=%s blob=%s error=%v", biosValidationID, biosSnapshotJSON, validationBIOSBlobID, err)
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
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return selected.Version != 6 }), "select uploaded review cover = %#v, error=%v", selected, err)
	approved, err := importer.Approve(ctx, itemID, 6)
	testassert.Falsef(t, err != nil, "approve: %v", err)
	var title, titleInitial, variantStatus, publishedCoverBlobID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT g.title,
g.title_initial,
v.status,
(SELECT blob_id FROM game_assets WHERE game_id=g.id AND kind='COVER')
FROM games g
JOIN game_variants v ON v.game_id=g.id
WHERE g.id=?
`, approved.GameID).Scan(&title, &titleInitial, &variantStatus, &publishedCoverBlobID); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(
		func() bool { return title != "Sudoku" },
		func() bool { return titleInitial != "S" },
		func() bool { return variantStatus != "READY" },
		func() bool { return publishedCoverBlobID != sourceBlobID },
	), "published title/initial/status/cover = %s/%s/%s/%s", title, titleInitial, variantStatus, publishedCoverBlobID)
	var publishedTags int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT count(*) FROM game_tags WHERE game_id=? AND tag_id=?
`, approved.GameID, defaultTag.TagID).Scan(&publishedTags); err != nil || publishedTags != 1 {
		t.Fatalf("published game tag = %d, %v", publishedTags, err)
	}
	discarded, err := importer.Discard(ctx, discardItemID, refreshedTransientDraft.Version, "")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return discarded.Status != "DISCARDED" }), "discard review = %#v, error=%v", discarded, err)
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
	testassert.Falsef(t, testassert.Any(func() bool { return discardedJobState != "COMPLETED" }, func() bool { return discardedJobPending != 0 }, func() bool { return discardedJobPublished != 1 }, func() bool { return discardedJobDiscarded != 1 }, func() bool { return discardedItemState != "DISCARDED" }), "discard aggregate = job:%s pending:%d published:%d discarded:%d item:%s", discardedJobState, discardedJobPending, discardedJobPublished, discardedJobDiscarded, discardedItemState)
	releases, err := payloadrelease.New(database.SQL, blobs, time.Now, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 10; attempt++ {
		worked, runErr := releases.RunOnce(ctx)
		if runErr != nil {
			t.Fatal(runErr)
		}
		if !worked {
			break
		}
	}
	var releasedItems, releasedJobs, purgedFiles, sourceRows, uploadedAssetRows, publishedPayloadRows int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT
 (SELECT count(*) FROM import_items WHERE import_job_id=? AND payload_state='RELEASED'),
 (SELECT count(*) FROM import_jobs WHERE id=? AND payload_state='RELEASED'),
 (SELECT count(*) FROM upload_files WHERE upload_session_id=? AND state='PURGED' AND final_blob_id IS NULL),
 (SELECT count(*) FROM import_item_source_files WHERE import_item_id IN (?,?)),
 (SELECT count(*) FROM review_uploaded_assets WHERE import_item_id IN (?,?)),
 (SELECT count(*) FROM game_files file WHERE file.game_id=?)+
 (SELECT count(*) FROM game_assets WHERE game_id=?)
`, created.ImportJobID, created.ImportJobID, upload.ID, itemID, discardItemID, itemID, discardItemID,
		approved.GameID, approved.GameID).Scan(
		&releasedItems, &releasedJobs, &purgedFiles, &sourceRows, &uploadedAssetRows, &publishedPayloadRows,
	); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(
		func() bool { return releasedItems != 2 }, func() bool { return releasedJobs != 1 },
		func() bool { return purgedFiles != 2 }, func() bool { return sourceRows != 0 },
		func() bool { return uploadedAssetRows != 0 }, func() bool { return publishedPayloadRows != 2 },
	), "released import = items:%d job:%d files:%d source:%d assets:%d published:%d",
		releasedItems, releasedJobs, purgedFiles, sourceRows, uploadedAssetRows, publishedPayloadRows)
	var publishedDiscard, retainedBlob int
	var beforeJSON, configEvidenceJSON, datEvidenceJSON string
	var discardReason sql.NullString
	if err := database.SQL.QueryRowContext(ctx, `
SELECT (SELECT count(*)
FROM games g
WHERE g.title='Discarded'),
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
		strings.Contains(beforeJSON, "sourceManifest") ||
		strings.Contains(beforeJSON, "selectedAssets") ||
		!strings.Contains(beforeJSON, `"name":"待通关"`) ||
		strings.Contains(configEvidenceJSON, "configSnapshot") ||
		strings.Contains(datEvidenceJSON, "dependencySnapshot") ||
		!strings.Contains(configEvidenceJSON, `"validationAvailable":true`) ||
		!strings.Contains(datEvidenceJSON, `"datMatched":false`) {
		t.Fatalf("discard evidence = games:%d blob:%d before:%s config:%s dat:%s error=%v", publishedDiscard, retainedBlob, beforeJSON, configEvidenceJSON, datEvidenceJSON, err)
	}
	if _, err := database.SQL.ExecContext(ctx, `UPDATE review_events SET reason='tampered' WHERE id=?`, discarded.EventID); err == nil ||
		!strings.Contains(err.Error(), "immutable") {
		t.Fatalf("immutable review event update error = %v", err)
	}
}
