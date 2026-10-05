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

	"retrom/internal/testsupport/testpostgres"

	"retrom/internal/persistence/contentquery"
	librarypersistence "retrom/internal/persistence/libraryimport"

	cleanupcomposition "retrom/internal/composition/cleanupjobs"
	dbapi "retrom/internal/database"

	uploadpersistence "retrom/internal/persistence/uploads"

	dependencypersistence "retrom/internal/persistence/dependencies"
	dependencyservice "retrom/internal/service/dependencies"

	"retrom/internal/persistence/recordstore"
	tagpersistence "retrom/internal/persistence/tagging"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	corevalidation "retrom/internal/core/validation"
	"retrom/internal/dependencies"
	"retrom/internal/filestore"
	"retrom/internal/importing"
	"retrom/internal/service/tagging"
	"retrom/internal/service/uploads"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
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
	database, err := testsupport.OpenDatabase(ctx, testpostgres.DSN(t), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	if err := dependencyservice.New(dependencySet,
		dependencypersistence.New(database.SQL)).Bootstrap(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	archiveBytes, err := os.ReadFile(filepath.Join(repositoryRoot, "internal", "importing",
		"testdata", "sevenzip", "single.7z"))
	testassert.False(t, err != nil, err)
	payloadBytes, err := os.ReadFile(filepath.Join(repositoryRoot, "internal", "importing",
		"testdata", "sevenzip", "payload", "game.a26"))
	testassert.False(t, err != nil, err)
	blobs, err := filestore.Open(dataDir)
	testassert.False(t, err != nil, err)
	uploadService := uploads.New(uploadpersistence.New(database.SQL), blobs, dataDir, time.Now)
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
		if err := dbapi.QueryRowContext(ctx, database.SQL, "SELECT state FROM jobs WHERE id=?",
			jobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" {
			break
		}
		testassert.Falsef(t, time.Now().After(deadline), "upload finalization = %s", state)
		time.Sleep(10 * time.Millisecond)
	}
	created, err := newTestImporter(t, database.SQL, blobs, testImportOptions{Now: time.Now}).Create(ctx, CreateRequest{
		UploadID:                 upload.ID,
		TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "atari2600/stella2014"),
		MetadataProvider:         "NONE",
	})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil },
		func() bool { return created.ItemCount != 1 }), "Create() = %#v, error=%v", created, err)
	var itemID, sourceArchiveFileRecord, contentFileRecord, logicalName, archiveFormat,
		compressionProfile, contentSHA string
	var sourceOrdinal int
	if err := dbapi.QueryRowContext(ctx, database.SQL, `
SELECT i.id,
       source.source_archive_file_record,
       source.source_archive_entry_ordinal,
       source.file_record,
       source.logical_name,
       entry.archive_format,
       entry.compression_profile,
       ((content.value)::jsonb #>> '{sha256}')
FROM import_items i
JOIN import_item_source_files source ON source.import_item_id=i.id AND source.role='CONTENT'
JOIN archive_entries entry ON entry.archive_file_record=source.source_archive_file_record
 AND entry.ordinal=source.source_archive_entry_ordinal
JOIN LATERAL (SELECT source.file_record AS value) content ON content.value IS NOT NULL
WHERE i.import_job_id=?
`, created.ImportJobID).Scan(
		&itemID,
		&sourceArchiveFileRecord,
		&sourceOrdinal,
		&contentFileRecord,
		&logicalName,
		&archiveFormat,
		&compressionProfile,
		&contentSHA,
	); err != nil {
		t.Fatal(err)
	}
	payloadDigest := sha256.Sum256(payloadBytes)
	testassert.Falsef(t, testassert.Any(func() bool { return sourceArchiveFileRecord == "" },
		func() bool { return contentFileRecord == sourceArchiveFileRecord },
		func() bool { return sourceOrdinal != 0 }, func() bool { return logicalName != "game.a26" },
		func() bool { return archiveFormat != "SEVEN_Z" },
		func() bool { return compressionProfile != "SEVEN_Z_DECODER_VALIDATED" },
		func() bool { return contentSHA != hex.EncodeToString(payloadDigest[:]) }),
		"materialized source = archive:%s ordinal:%d content:%s name:%s format:%s/%s sha:%s",
		sourceArchiveFileRecord, sourceOrdinal, contentFileRecord, logicalName, archiveFormat,
		compressionProfile, contentSHA)
	approved, err := newTestImporter(t, database.SQL, blobs, testImportOptions{Now: time.Now}).Approve(ctx, itemID, 1)
	testassert.False(t, err != nil, err)
	var publishedFileRecord string
	var publishedArchiveID sql.NullString
	var publishedOrdinal sql.NullInt64
	if err := dbapi.QueryRowContext(ctx, database.SQL, `
SELECT file.file_record,file.source_archive_file_record,file.source_archive_entry_ordinal
FROM games game
JOIN game_files file ON file.game_id=game.id
WHERE game.id=? AND file.role='CONTENT'
`, approved.GameID).Scan(&publishedFileRecord, &publishedArchiveID, &publishedOrdinal); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t,
		testassert.Any(func() bool { return publishedFileRecord == contentFileRecord },
			func() bool { return publishedArchiveID.Valid },
			func() bool { return publishedOrdinal.Valid }), "published source = %s/%v/%v",
		publishedFileRecord, publishedArchiveID, publishedOrdinal)
}

func TestUploadImportReviewPublishPipeline(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, testpostgres.DSN(t), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	if err := dependencyservice.New(dependencySet,
		dependencypersistence.New(database.SQL)).Bootstrap(ctx, time.Now()); err != nil {
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
	defaultTag, err := tagging.New(tagpersistence.New(database.SQL), time.Now).Create(ctx, adminID, "待通关")
	testassert.False(t, err != nil, err)
	blobs, _ := filestore.Open(dataDir)
	uploadService := uploads.New(uploadpersistence.New(database.SQL), blobs, dataDir, time.Now)
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
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0,
		fmt.Sprintf("bytes 0-%d/%d", len(contents)-2, len(contents)), digestHeader,
		bytes.NewReader(contents)); err == nil {
		t.Fatal("range/body length mismatch succeeded")
	}
	rangeHeader := "bytes 0-24/25"
	testassert.Falsef(t, len(contents) != 25, "fixture size changed: %d", len(contents))
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0, rangeHeader,
		digestHeader, bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[1].ID, 0, rangeHeader,
		digestHeader, bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	current, _ := uploadService.Get(ctx, upload.ID)
	jobID, _, err := uploadService.Complete(ctx, upload.ID, current.Version)
	testassert.False(t, err != nil, err)
	deadline := time.Now().Add(3 * time.Second)
	for {
		var state string
		_ = dbapi.QueryRowContext(ctx, database.SQL, "SELECT state FROM jobs WHERE id=?", jobID).Scan(&state)
		if state == "SUCCEEDED" {
			break
		}
		testassert.Falsef(t, time.Now().After(deadline), "upload finalization = %s", state)
		time.Sleep(10 * time.Millisecond)
	}
	created, err := newTestImporter(t, database.SQL, blobs, testImportOptions{Now: time.Now}).
		Create(ctx, CreateRequest{
			UploadID:                 upload.ID,
			TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "gba/mgba"),
			MetadataProvider:         "NONE", TagIDs: []string{defaultTag.TagID},
		})
	testassert.Falsef(t, err != nil, "create import: %v", err)
	var itemID string
	if err := dbapi.QueryRowContext(ctx, database.SQL, `
SELECT i.id
FROM import_items i
JOIN import_item_source_files f ON f.import_item_id=i.id
WHERE i.import_job_id=?
AND f.logical_name='Sudoku.gba'
`, created.ImportJobID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	var discardItemID, discardFileRecord string
	if err := dbapi.QueryRowContext(ctx, database.SQL, `
SELECT i.id,
f.file_record
FROM import_items i
JOIN import_item_source_files f ON f.import_item_id=i.id
WHERE i.import_job_id=?
AND f.logical_name='Discarded.gba'
	`, created.ImportJobID).Scan(&discardItemID, &discardFileRecord); err != nil {
		t.Fatal(err)
	}
	var inheritedDrafts int
	var initialConfigSnapshot string
	if err := dbapi.QueryRowContext(ctx, database.SQL, `
SELECT count(DISTINCT draft.id),job.config_snapshot_json
FROM import_jobs job
JOIN import_items item ON item.import_job_id=job.id
JOIN import_items draft ON draft.id=item.id
JOIN review_draft_tags relation ON relation.review_draft_id=draft.id AND relation.tag_id=?
WHERE job.id=? GROUP BY job.id
`, defaultTag.TagID, created.ImportJobID).Scan(&inheritedDrafts, &initialConfigSnapshot); err != nil ||
		inheritedDrafts != 2 || !strings.Contains(initialConfigSnapshot, `"name":"待通关"`) {
		t.Fatalf("default tag inheritance = drafts:%d config:%s error:%v", inheritedDrafts, initialConfigSnapshot, err)
	}
	importer := newTestImporter(t, database.SQL, blobs, testImportOptions{Now: time.Now})
	transientTag, err := tagging.New(tagpersistence.New(database.SQL), time.Now).Create(ctx, adminID, "删除失效")
	testassert.False(t, err != nil, err)
	transientDraft, err := importer.PatchDraft(ctx, discardItemID, 1, DraftPatch{
		TagIDs: []string{defaultTag.TagID, transientTag.TagID},
	})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil },
		func() bool { return transientDraft.Version != 2 }), "add transient review tag = %#v, %v",
		transientDraft, err)
	currentTransientTag, err := tagging.New(tagpersistence.New(database.SQL), time.Now).Get(ctx, transientTag.TagID)
	testassert.False(t, err != nil, err)
	if _, _, err := tagging.New(tagpersistence.New(database.SQL), time.Now).Delete(
		ctx, adminID, transientTag.TagID, transientTag.Name, currentTransientTag.Version,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := importer.PatchDraft(ctx, discardItemID, 2,
		DraftPatch{TagIDs: []string{defaultTag.TagID}}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale review after tag delete error = %v", err)
	}
	refreshedTransientDraft, err := importer.PatchDraft(
		ctx, discardItemID, 3, DraftPatch{TagIDs: []string{defaultTag.TagID}},
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil },
		func() bool { return refreshedTransientDraft.Version != 4 },
		func() bool { return len(refreshedTransientDraft.Tags) != 1 },
		func() bool { return refreshedTransientDraft.Tags[0].TagID != defaultTag.TagID }),
		"refresh deleted review tag = %#v, %v", refreshedTransientDraft, err)
	var metadataPatch DraftPatch
	if err := json.Unmarshal([]byte(`{"metadata":{"players":2,"releaseYear":2001},"tagIds":[]}`), &metadataPatch); err != nil {
		t.Fatal(err)
	}
	patched, err := importer.PatchDraft(ctx, itemID, 1, metadataPatch)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil },
		func() bool { return patched.Version != 2 },
		func() bool { return patched.Metadata["players"] != int64(2) },
		func() bool { return patched.Metadata["releaseYear"] != int64(2001) }),
		"patch nullable metadata values = %#v, error=%v", patched, err)
	if err := json.Unmarshal([]byte(`{"metadata":{"players":null,"releaseYear":null},"tagIds":[]}`), &metadataPatch); err != nil {
		t.Fatal(err)
	}
	patched, err = importer.PatchDraft(ctx, itemID, 2, metadataPatch)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil },
		func() bool { return patched.Version != 3 },
		func() bool { return patched.Metadata["players"] != nil },
		func() bool { return patched.Metadata["releaseYear"] != nil }),
		"clear nullable metadata values = %#v, error=%v", patched, err)
	crossPlatform := testsupport.MustPlatformInstanceID(t, database.SQL, "nes/fceumm")
	_, err = importer.PatchDraft(ctx, itemID, 3, DraftPatch{TargetPlatformInstanceID: &crossPlatform, TagIDs: []string{}})
	testassert.Truef(t, errors.Is(err, ErrReimportRequiredPlatformChange), "cross-platform draft change error = %v", err)
	var importConfigSnapshot string
	if err := dbapi.QueryRowContext(ctx, database.SQL, `
SELECT j.config_snapshot_json
FROM import_items d
JOIN import_items i ON i.id=d.id
JOIN import_jobs j ON j.id=i.import_job_id
WHERE d.id=?
`, itemID).Scan(&importConfigSnapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := recordstore.UpdatePlatformInstances(ctx, database.SQL, recordstore.Update{
		Set: `
version=version+1,
updated_at_ms=updated_at_ms+1
`,
		Scope: recordstore.Scope{
			Where: `catalog_template_key='gba/mgba'`,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"metadata":{"title":"Sudoku"},"tagIds":[]}`), &metadataPatch); err != nil {
		t.Fatal(err)
	}
	refreshed, err := importer.PatchDraft(ctx, itemID, 3, metadataPatch)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil },
		func() bool { return refreshed.Version != 4 }), "refresh config validation = %#v, error=%v",
		refreshed, err)
	var refreshedPlatformVersion int64
	if err := dbapi.QueryRowContext(ctx, database.SQL, `
SELECT v.platform_instance_version
FROM import_items d
JOIN (`+contentquery.CurrentContentSQL+`) v ON v.import_item_id=d.id
WHERE d.id=?
	`, itemID).Scan(&refreshedPlatformVersion); err != nil ||
		refreshedPlatformVersion != 2 ||
		!strings.Contains(importConfigSnapshot, `"platformInstanceVersion":1`) {
		t.Fatalf("current platform version=%d frozen import config=%s error=%v",
			refreshedPlatformVersion, importConfigSnapshot, err)
	}
	var sourceFileRecord string
	if err := dbapi.QueryRowContext(ctx, database.SQL,
		"SELECT final_file_record FROM upload_files WHERE id=?",
		upload.Files[0].ID).Scan(&sourceFileRecord); err != nil {
		t.Fatal(err)
	}
	var requirementID, md5Value, sha1Value, sha256Value string
	var requirementVersion, sourceSize int64
	if err := dbapi.QueryRowContext(ctx, database.SQL, `
SELECT id,version
FROM bios_requirements
WHERE core_id='mgba' AND logical_name='gba_bios.bin' AND enabled=1
`).Scan(&requirementID, &requirementVersion); err != nil {
		t.Fatal(err)
	}
	sourceRecord, err := filestore.ParseRecord(sourceFileRecord)
	testassert.False(t, err != nil, err)
	sourceSize, md5Value, sha1Value, sha256Value = sourceRecord.Size, sourceRecord.MD5,
		sourceRecord.SHA1, sourceRecord.SHA256

	const biosInstallationID = "01990000-0000-7000-8000-000000000010"
	if _, err := recordstore.InsertRows(ctx, database.SQL, "bios_installations", `
INSERT INTO bios_installations(id,requirement_id,file_record,original_filename,size_bytes,md5,sha1,sha256,
validated_requirement_version,status,validation_details_json,is_active,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,'HASH_WARNING','{}',1,1,?,?)
`, biosInstallationID, requirementID, sourceFileRecord, "gba_bios.bin", sourceSize, md5Value, sha1Value, sha256Value, requirementVersion, time.Now().UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"metadata":{"description":"BIOS snapshot refreshed"},"tagIds":[]}`), &metadataPatch); err != nil {
		t.Fatal(err)
	}
	biosRefreshed, err := importer.PatchDraft(ctx, itemID, 4, metadataPatch)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil },
		func() bool { return biosRefreshed.Version != 5 }), "refresh BIOS validation = %#v, error=%v",
		biosRefreshed, err)
	currentRuntime, err := librarypersistence.ReadReviewRuntime(ctx, database.SQL, itemID)
	if err != nil {
		t.Fatal(err)
	}
	biosSnapshot, err := corevalidation.ParseSnapshot(currentRuntime.DependencyJSON)
	if err != nil || len(biosSnapshot.BIOS) != 1 || biosSnapshot.BIOS[0].InstallationID == nil || *biosSnapshot.BIOS[0].InstallationID != biosInstallationID || biosSnapshot.BIOS[0].FileRecord == nil || *biosSnapshot.BIOS[0].FileRecord != sourceFileRecord {
		t.Fatalf("current BIOS facts = %s error=%v", currentRuntime.DependencyJSON, err)
	}
	coverMetadata, err := blobs.Copy(ctx, sourceFileRecord)
	testassert.False(t, err != nil, err)
	coverFileRecord, err := filestore.FileRecord(coverMetadata, "image/png")
	testassert.False(t, err != nil, err)

	manualCoverID := "01990000-0000-7000-8000-000000000001"
	if _, err := recordstore.InsertRows(ctx, database.SQL, "review_uploaded_assets", `
INSERT INTO review_uploaded_assets(id,import_item_id,upload_file_id,file_record,kind,width_px,height_px,
media_type,created_at_ms)
VALUES(?,?,?,?,'COVER',600,900,'image/png',?)
`, manualCoverID, itemID, upload.Files[0].ID, coverFileRecord, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	selected, err := importer.PatchDraft(ctx, itemID, 5, DraftPatch{
		SelectedAssets: &SelectedAssets{CoverUploadedAssetID: &manualCoverID}, TagIDs: []string{defaultTag.TagID},
	})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil },
		func() bool { return selected.Version != 6 }), "select uploaded review cover = %#v, error=%v",
		selected, err)
	approved, err := importer.Approve(ctx, itemID, 6)
	testassert.Falsef(t, err != nil, "approve: %v", err)
	var title, titleInitial, variantStatus, publishedCoverFileRecord string
	if err := dbapi.QueryRowContext(ctx, database.SQL, `
SELECT g.title,
g.title_initial,
v.status,
(SELECT file_record FROM game_assets WHERE game_id=g.id AND kind='COVER')
FROM games g
JOIN game_variants v ON v.game_id=g.id
WHERE g.id=?
`, approved.GameID).Scan(&title, &titleInitial, &variantStatus, &publishedCoverFileRecord); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(
		func() bool { return title != "Sudoku" },
		func() bool { return titleInitial != "S" },
		func() bool { return variantStatus != "READY" },
		func() bool { return publishedCoverFileRecord == coverFileRecord },
	), "published title/initial/status/cover = %s/%s/%s/%s", title, titleInitial, variantStatus, publishedCoverFileRecord)
	var publishedTags int
	if err := dbapi.QueryRowContext(ctx, database.SQL, `
SELECT count(*) FROM game_tags WHERE game_id=? AND tag_id=?
`, approved.GameID, defaultTag.TagID).Scan(&publishedTags); err != nil || publishedTags != 1 {
		t.Fatalf("published game tag = %d, %v", publishedTags, err)
	}
	discarded, err := importer.Discard(ctx, discardItemID, refreshedTransientDraft.Version, "")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil },
		func() bool { return discarded.Status != "DISCARDED" }), "discard review = %#v, error=%v",
		discarded, err)
	var discardedJobState, discardedItemState string
	var discardedJobPending, discardedJobPublished, discardedJobDiscarded int64
	if err := dbapi.QueryRowContext(ctx, database.SQL, `
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
	testassert.Falsef(t, testassert.Any(func() bool { return discardedJobState != "COMPLETED" },
		func() bool { return discardedJobPending != 0 },
		func() bool { return discardedJobPublished != 1 },
		func() bool { return discardedJobDiscarded != 1 },
		func() bool { return discardedItemState != "DISCARDED" }),
		"discard aggregate = job:%s pending:%d published:%d discarded:%d item:%s", discardedJobState,
		discardedJobPending, discardedJobPublished, discardedJobDiscarded, discardedItemState)
	releases, err := cleanupcomposition.New(ctx, database.SQL, blobs, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releases.Close)
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
	if err := dbapi.QueryRowContext(ctx, database.SQL, `
SELECT
 (SELECT count(*) FROM import_items WHERE import_job_id=? AND payload_state='RELEASED'),
 (SELECT count(*) FROM import_jobs WHERE id=? AND payload_state='RELEASED'),
 (SELECT count(*) FROM upload_files WHERE upload_session_id=? AND state='PURGED' AND final_file_record
IS NULL),
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
	if err := dbapi.QueryRowContext(ctx, database.SQL, `SELECT (SELECT count(*) FROM games WHERE title='Discarded'),(SELECT count(*) FROM import_files WHERE
file_record=?)`, discardFileRecord).Scan(&publishedDiscard, &retainedBlob); err != nil || publishedDiscard != 0 || retainedBlob != 0 {
		t.Fatalf("discard payload: %d %d %v", publishedDiscard, retainedBlob, err)
	}
}
