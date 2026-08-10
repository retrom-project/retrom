//go:build integration

package libraryimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/store"
	"retrom/internal/uploads"
)

type multiDiscUploadFile struct {
	path     string
	contents []byte
}

func completeMultiDiscDirectory(
	t *testing.T,
	ctx context.Context,
	database *store.DB,
	blobs *blobstore.Store,
	dataDir string,
	files []multiDiscUploadFile,
) string {
	t.Helper()
	declarations := make([]uploads.FileDeclaration, 0, len(files))
	for index, file := range files {
		declarations = append(declarations, uploads.FileDeclaration{
			ClientFileID: fmt.Sprintf("file-%d", index), RelativePath: file.path, SizeBytes: int64(len(file.contents)),
		})
	}
	service := uploads.New(database.SQL, blobs, dataDir, time.Now)
	upload, err := service.Create(ctx, uploads.CreateRequest{SourceType: "DIRECTORY", Files: declarations})
	if err != nil {
		t.Fatal(err)
	}
	for index, file := range files {
		digest := sha256.Sum256(file.contents)
		if err := service.PutPart(
			ctx,
			upload.ID,
			upload.Files[index].ID,
			0,
			fmt.Sprintf("bytes 0-%d/%d", len(file.contents)-1, len(file.contents)),
			"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":",
			bytes.NewReader(file.contents),
		); err != nil {
			t.Fatal(err)
		}
	}
	current, err := service.Get(ctx, upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _, err := service.Complete(ctx, upload.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	waitParentJob(t, database.SQL, jobID, "SUCCEEDED")
	return upload.ID
}

func newMultiDiscImportFixture(t *testing.T) (context.Context, string, *store.DB, *blobstore.Store, *Service) {
	t.Helper()
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
	bios, err := blobs.Put(bytes.NewReader([]byte("deterministic invalid Saturn BIOS fixture")))
	if err != nil {
		t.Fatal(err)
	}
	biosBlobID, err := blobstore.EnsureRecord(ctx, database.SQL, bios, "application/octet-stream", time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	var requirementID string
	var requirementVersion int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id,version FROM bios_requirements
WHERE core_id='yabause' AND logical_name='saturn_bios.bin' AND enabled=1
`).Scan(&requirementID, &requirementVersion); err != nil {
		t.Fatal(err)
	}
	installationID := "01990000-0000-7000-8000-" + requirementID[len(requirementID)-12:]
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO bios_installations(id,requirement_id,blob_id,original_filename,size_bytes,md5,sha1,sha256,
validated_requirement_version,status,validation_details_json,is_active,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,'HASH_WARNING','{}',1,1,?,?)
`, installationID, requirementID, biosBlobID, "saturn_bios.bin", bios.Size, bios.MD5, bios.SHA1, bios.SHA256,
		requirementVersion, time.Now().UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	importer := New(database.SQL, time.Now).WithBlobStore(blobs).WithMultiDiscImportEnabled(true)
	return ctx, dataDir, database, blobs, importer
}

func fakeCHD(payload string) []byte {
	return append([]byte("MComprHD"), []byte(payload)...)
}

func TestMultiDiscDirectoryCreatesOrderedItemsAndPublishesCanonicalContent(t *testing.T) {
	t.Parallel()
	ctx, dataDir, database, blobs, importer := newMultiDiscImportFixture(t)
	uploadID := completeMultiDiscDirectory(t, ctx, database, blobs, dataDir, []multiDiscUploadFile{
		{path: "Alpha/original.m3u", contents: []byte("Disc One.CHD\nDisc Two.chd\n")},
		{path: "Alpha/disc one.chd", contents: fakeCHD("alpha-one")},
		{path: "Alpha/Disc Two.chd", contents: fakeCHD("alpha-two")},
		{path: "Alpha/notes.txt", contents: []byte("ignored")},
		{path: "Alpha/unreferenced.chd", contents: []byte("tiny")},
		{path: "Nested/Beta/game.m3u", contents: []byte("one.chd\ntwo.chd\n")},
		{path: "Nested/Beta/one.chd", contents: fakeCHD("beta-one")},
		{path: "Nested/Beta/two.chd", contents: fakeCHD("beta-two")},
	})
	created, err := importer.Create(ctx, CreateRequest{
		UploadID: uploadID, TargetPlatformInstanceID: "01980000-0000-7000-8000-000000000020",
		MetadataProvider: "NONE", ContentMode: "MULTI_DISC_M3U_V1",
	})
	if err != nil || created.ItemCount != 2 {
		t.Fatalf("Create() = %#v, error=%v", created, err)
	}
	items := queryAttachmentStrings(t, database.SQL, `
SELECT item.id||':'||snapshot.content_kind||':'||validation.prepublish_generation
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
JOIN import_item_core_validations validation ON validation.id=draft.selected_validation_id
WHERE item.import_job_id=? ORDER BY item.group_key
`, created.ImportJobID)
	if len(items) != 2 {
		t.Fatalf("items = %#v", items)
	}
	var firstItemID, firstSnapshotID string
	if err := database.SQL.QueryRow(`
SELECT item.id,snapshot.id
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
WHERE item.import_job_id=?
AND EXISTS(
  SELECT 1 FROM import_item_source_snapshot_files file
  WHERE file.source_snapshot_id=snapshot.id
  AND file.role='PLAYLIST_SOURCE'
  AND file.logical_name='original.m3u'
)
`, created.ImportJobID).Scan(&firstItemID, &firstSnapshotID); err != nil {
		t.Fatal(err)
	}
	entries := queryAttachmentStrings(t, database.SQL, `
SELECT printf('%d:%s:%s:%s',ordinal,state,normalized_reference,canonical_name)
FROM import_item_multidisc_entries WHERE source_snapshot_id=? ORDER BY ordinal
`, firstSnapshotID)
	if fmt.Sprint(entries) != "[0:PRESENT:disc one.chd:disc-001.chd 1:PRESENT:disc two.chd:disc-002.chd]" {
		t.Fatalf("entries = %v", entries)
	}
	var playlistSHA string
	if err := database.SQL.QueryRow(`
SELECT blob.sha256
FROM import_item_core_validations validation
JOIN import_item_validation_files file ON file.import_item_core_validation_id=validation.id
JOIN blobs blob ON blob.id=file.blob_id
WHERE validation.source_snapshot_id=? AND file.role='MULTI_DISC_PLAYLIST'
`, firstSnapshotID).Scan(&playlistSHA); err != nil {
		t.Fatal(err)
	}
	reader, err := blobs.OpenDigest(playlistSHA)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := io.ReadAll(reader)
	cleanup.Error("close", reader.Close())
	if err != nil || string(canonical) != "disc-001.chd\ndisc-002.chd\n" {
		t.Fatalf("canonical playlist = %q, error=%v", canonical, err)
	}
	var ignored int
	if err := database.SQL.QueryRow(`
SELECT count(*) FROM import_job_files WHERE import_job_id=?
AND disposition='IGNORED' AND reason_code='NOT_REFERENCED_BY_PLAYLIST'
	`, created.ImportJobID).Scan(&ignored); err != nil || ignored != 2 {
		t.Fatalf("ignored files = %d, error=%v", ignored, err)
	}
	approved, err := importer.Approve(ctx, firstItemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	published := queryAttachmentStrings(t, database.SQL, `
SELECT revision.content_kind||':'||file.role||':'||printf('%d',file.sort_order)
FROM games game
JOIN game_content_revisions revision ON revision.id=game.current_content_revision_id
JOIN game_content_files file ON file.game_content_revision_id=revision.id
WHERE game.id=? ORDER BY file.role,file.sort_order
`, approved.GameID)
	if len(published) != 3 {
		t.Fatalf("published content = %v", published)
	}
}

func TestMultiDiscMissingDiscIsBlockedWithoutPlaceholderBlob(t *testing.T) {
	t.Parallel()
	ctx, dataDir, database, blobs, importer := newMultiDiscImportFixture(t)
	uploadID := completeMultiDiscDirectory(t, ctx, database, blobs, dataDir, []multiDiscUploadFile{
		{path: "game/game.m3u", contents: []byte("one.chd\ntwo.chd\nthree.chd\n")},
		{path: "game/one.chd", contents: fakeCHD("one")},
		{path: "game/two.chd", contents: fakeCHD("two")},
	})
	created, err := importer.Create(ctx, CreateRequest{
		UploadID: uploadID, TargetPlatformInstanceID: "01980000-0000-7000-8000-000000000020",
		MetadataProvider: "NONE", ContentMode: "MULTI_DISC_M3U_V1",
	})
	if err != nil || created.ItemCount != 1 {
		t.Fatalf("Create() = %#v, error=%v", created, err)
	}
	var itemID, validationStatus, compatibilityCode string
	var selectedValidationID *string
	var missingBlobID, missingUploadID *string
	if err := database.SQL.QueryRow(`
SELECT item.id,validation.status,validation.compatibility_code,draft.selected_validation_id,
entry.blob_id,entry.upload_file_id
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
JOIN import_item_core_validations validation ON validation.source_snapshot_id=snapshot.id
JOIN import_item_multidisc_entries entry ON entry.source_snapshot_id=snapshot.id AND entry.state='MISSING'
WHERE item.import_job_id=?
`, created.ImportJobID).Scan(
		&itemID, &validationStatus, &compatibilityCode, &selectedValidationID, &missingBlobID, &missingUploadID,
	); err != nil {
		t.Fatal(err)
	}
	if validationStatus != "BLOCKED" || compatibilityCode != "MULTI_DISC_FILE_MISSING" ||
		selectedValidationID != nil || missingBlobID != nil || missingUploadID != nil {
		t.Fatalf("blocked item = %s/%s selected=%v blob=%v upload=%v",
			validationStatus, compatibilityCode, selectedValidationID, missingBlobID, missingUploadID)
	}
	if _, err := importer.Approve(ctx, itemID, 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blocked approve error = %v", err)
	}
}

func TestMultiDiscAdmissionRejectsMissingPlaylistAndUnsupportedTargetWithoutConsumption(t *testing.T) {
	t.Parallel()
	ctx, dataDir, database, blobs, importer := newMultiDiscImportFixture(t)
	uploadID := completeMultiDiscDirectory(t, ctx, database, blobs, dataDir, []multiDiscUploadFile{
		{path: "game/one.chd", contents: fakeCHD("one")},
		{path: "game/two.chd", contents: fakeCHD("two")},
	})
	request := CreateRequest{
		UploadID: uploadID, TargetPlatformInstanceID: "01980000-0000-7000-8000-000000000020",
		MetadataProvider: "NONE", ContentMode: "MULTI_DISC_M3U_V1",
	}
	if _, err := importer.Create(ctx, request); !errors.Is(err, ErrMultiDiscPlaylistMissing) {
		t.Fatalf("missing playlist error = %v", err)
	}
	request.TargetPlatformInstanceID = "01980000-0000-7000-8000-000000000019"
	if _, err := importer.Create(ctx, request); !errors.Is(err, ErrMultiDiscModeUnavailable) {
		t.Fatalf("unsupported target error = %v", err)
	}
	var imports, consumptions int
	if err := database.SQL.QueryRow(`
SELECT (SELECT count(*) FROM import_jobs WHERE upload_session_id=?),
       (SELECT count(*) FROM upload_consumptions WHERE upload_session_id=?)
`, uploadID, uploadID).Scan(&imports, &consumptions); err != nil || imports != 0 || consumptions != 0 {
		t.Fatalf("rejected admission wrote imports=%d consumptions=%d error=%v", imports, consumptions, err)
	}
}
