//go:build integration

package libraryimport

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/importing"
	"retrom/internal/store"
	"retrom/internal/uploads"
)

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
		Create(ctx, CreateRequest{UploadID: upload.ID, TargetPlatformInstanceID: "01980000-0000-7000-8000-000000000005", MetadataProvider: "NONE"})
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
	importer := New(database.SQL, time.Now)
	var metadataPatch DraftPatch
	if err := json.Unmarshal([]byte(`{"metadata":{"players":2,"releaseYear":2001}}`), &metadataPatch); err != nil {
		t.Fatal(err)
	}
	patched, err := importer.PatchDraft(ctx, itemID, 1, metadataPatch)
	if err != nil || patched.Version != 2 || patched.Metadata["players"] != int64(2) ||
		patched.Metadata["releaseYear"] != int64(2001) {
		t.Fatalf("patch nullable metadata values = %#v, error=%v", patched, err)
	}
	if err := json.Unmarshal([]byte(`{"metadata":{"players":null,"releaseYear":null}}`), &metadataPatch); err != nil {
		t.Fatal(err)
	}
	patched, err = importer.PatchDraft(ctx, itemID, 2, metadataPatch)
	if err != nil || patched.Version != 3 || patched.Metadata["players"] != nil ||
		patched.Metadata["releaseYear"] != nil {
		t.Fatalf("clear nullable metadata values = %#v, error=%v", patched, err)
	}
	crossPlatform := "01980000-0000-7000-8000-000000000001"
	_, err = importer.PatchDraft(ctx, itemID, 3, DraftPatch{TargetPlatformInstanceID: &crossPlatform})
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
	if err := json.Unmarshal([]byte(`{"metadata":{"title":"Sudoku"}}`), &metadataPatch); err != nil {
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
	manualCoverID := "01990000-0000-7000-8000-000000000001"
	var sourceBlobID string
	if err := database.SQL.QueryRowContext(ctx, "SELECT final_blob_id FROM upload_files WHERE id=?", upload.Files[0].ID).Scan(&sourceBlobID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO review_uploaded_assets(id,import_item_id,upload_file_id,blob_id,kind,width_px,height_px,media_type,created_at_ms)
VALUES(?,?,?,?,'COVER',600,900,'image/png',?)
`, manualCoverID, itemID, upload.Files[0].ID, sourceBlobID, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	selected, err := importer.PatchDraft(ctx, itemID, 4, DraftPatch{SelectedAssets: &SelectedAssets{CoverUploadedAssetID: &manualCoverID}})
	if err != nil || selected.Version != 5 {
		t.Fatalf("select uploaded review cover = %#v, error=%v", selected, err)
	}
	approved, err := importer.Approve(ctx, itemID, 5)
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
	discarded, err := importer.Discard(ctx, discardItemID, 1, "")
	if err != nil || discarded.Status != "DISCARDED" {
		t.Fatalf("discard review = %#v, error=%v", discarded, err)
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
		!strings.Contains(configEvidenceJSON, "configSnapshot") ||
		!strings.Contains(datEvidenceJSON, "dependencySnapshot") {
		t.Fatalf("discard evidence = games:%d blob:%d before:%s config:%s dat:%s error=%v", publishedDiscard, retainedBlob, beforeJSON, configEvidenceJSON, datEvidenceJSON, err)
	}
	if _, err := database.SQL.ExecContext(ctx, `UPDATE review_events SET reason='tampered' WHERE id=?`, discarded.EventID); err == nil ||
		!strings.Contains(err.Error(), "immutable") {
		t.Fatalf("immutable review event update error = %v", err)
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
		"Wrapped.zip": archive,
		"notes.txt":   []byte("unsupported"),
		".DS_Store":   []byte("sidecar"),
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
	if len(dispositions) != 3 || len(groups) != 1 || len(groups[0].sources) != 3 || len(groups[0].dosEntries) != 2 ||
		groups[0].defaultDOSEntry != "GAME/DOOM.EXE" ||
		groups[0].bundle == nil {
		t.Fatalf("DOS grouping = dispositions:%d groups:%#v", len(dispositions), groups)
	}
	if !groups[0].dosEntries[0].safe || groups[0].dosEntries[1].safe {
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
	dummyID, err := blobstore.EnsureRecord(ctx, database.SQL, dummy, "application/xml", time.Now().UnixMilli())
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
blob_id,
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
'USER',
?,
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
		dummyID,
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
	if len(dispositions) != 3 || len(groups) != 2 || len(archives) != 3 {
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
