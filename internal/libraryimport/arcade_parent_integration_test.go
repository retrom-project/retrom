//go:build integration

package libraryimport

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // DAT compatibility checksum.
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/launch"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/store"
	"retrom/internal/uploads"
)

func TestArcadeParentAttachmentsAdvanceImmutableSnapshotsUntilReadyAndPublish(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	if _, err := database.SQL.Exec(`INSERT INTO profiles(id,display_name,created_at_ms) VALUES('local','Fixture',0)`); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	insertArcadeParentCatalog(t, database.SQL)
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	childZIP := arcadeZIP(t, "a.bin", []byte("child"))
	parentZIP := arcadeZIP(t, "b.bin", []byte("parent"))
	rootZIP := arcadeZIP(t, "c.bin", []byte("root"))
	wrongZIP := arcadeZIP(t, "wrong.bin", []byte("wrong"))
	child := uploadCompleteFile(t, ctx, database.SQL, uploadService, "a.zip", childZIP)
	importer := New(database.SQL, time.Now).WithBlobStore(blobs)
	created, err := importer.Create(ctx, CreateRequest{
		UploadID: child.uploadID, TargetPlatformInstanceID: "01980000-0000-7000-8000-000000000006",
		MetadataProvider: "NONE",
	})
	if err != nil || created.ItemCount != 1 {
		t.Fatalf("child import = %#v, error=%v", created, err)
	}
	itemID, version, snapshotID, validationID := reviewAttachmentInputs(t, database.SQL, created.ImportJobID)
	view, found, err := importer.ReviewArcadeDependencies(ctx, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("arcade dependencies were not projected")
	}
	viewMap := view.(map[string]any)
	if viewMap["machine"] != "a" || len(viewMap["nodes"].([]map[string]any)) != 2 {
		t.Fatalf("initial dependency view = %#v", view)
	}
	parent := uploadCompleteFile(t, ctx, database.SQL, uploadService, "anything.zip", parentZIP)
	acceptedB, err := importer.CreateArcadeParentAttachment(ctx, itemID, version, ParentAttachmentRequest{
		ValidationID: validationID, BaseSourceSnapshotID: snapshotID, DependencyMachine: "b",
		UploadFileID: parent.fileID,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitParentJob(t, database.SQL, acceptedB.JobID, "SUCCEEDED")
	itemID, version, snapshotID, validationID = reviewAttachmentInputs(t, database.SQL, created.ImportJobID)
	if version != 3 {
		t.Fatalf("draft version after b = %d", version)
	}
	var revision, snapshotCount int
	var validationStatus, validationCode string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT snapshot.revision_no,validation.status,validation.compatibility_code,
(SELECT count(*) FROM import_item_source_snapshots WHERE import_item_id=snapshot.import_item_id)
FROM import_item_source_snapshots snapshot
JOIN import_item_core_validations validation ON validation.source_snapshot_id=snapshot.id
WHERE snapshot.id=? ORDER BY validation.created_at_ms DESC LIMIT 1
`, snapshotID).Scan(&revision, &validationStatus, &validationCode, &snapshotCount); err != nil {
		t.Fatal(err)
	}
	if revision != 2 || snapshotCount != 2 || validationStatus != "BLOCKED" || validationCode != "LAUNCH_PARENT_MISSING" {
		t.Fatalf("after b = revision:%d count:%d validation:%s/%s", revision, snapshotCount, validationStatus, validationCode)
	}
	wrong := uploadCompleteFile(t, ctx, database.SQL, uploadService, "c.zip", wrongZIP)
	rejectedC, err := importer.CreateArcadeParentAttachment(ctx, itemID, version, ParentAttachmentRequest{
		ValidationID: validationID, BaseSourceSnapshotID: snapshotID, DependencyMachine: "c",
		UploadFileID: wrong.fileID,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitParentJob(t, database.SQL, rejectedC.JobID, "FAILED")
	var attachmentState, attachmentCode, currentSnapshotID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT attachment.state,attachment.error_code,draft.effective_source_snapshot_id
FROM review_arcade_parent_attachments attachment
JOIN review_drafts draft ON draft.import_item_id=attachment.import_item_id
WHERE attachment.id=?
`, rejectedC.AttachmentID).Scan(&attachmentState, &attachmentCode, &currentSnapshotID); err != nil {
		t.Fatal(err)
	}
	if attachmentState != "REJECTED" || attachmentCode != ParentErrorMismatch || currentSnapshotID != snapshotID {
		t.Fatalf("wrong c = %s/%s snapshot=%s", attachmentState, attachmentCode, currentSnapshotID)
	}
	itemID, version, snapshotID, validationID = reviewAttachmentInputs(t, database.SQL, created.ImportJobID)
	root := uploadCompleteFile(t, ctx, database.SQL, uploadService, "renamed-root.zip", rootZIP)
	acceptedC, err := importer.CreateArcadeParentAttachment(ctx, itemID, version, ParentAttachmentRequest{
		ValidationID: validationID, BaseSourceSnapshotID: snapshotID, DependencyMachine: "c",
		UploadFileID: root.fileID,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitParentJob(t, database.SQL, acceptedC.JobID, "SUCCEEDED")
	itemID, version, snapshotID, validationID = reviewAttachmentInputs(t, database.SQL, created.ImportJobID)
	if version != 6 {
		t.Fatalf("draft version after c = %d", version)
	}
	if err := database.SQL.QueryRowContext(ctx, `
SELECT revision_no FROM import_item_source_snapshots WHERE id=?
`, snapshotID).Scan(&revision); err != nil || revision != 3 {
		t.Fatalf("final snapshot revision = %d, error=%v", revision, err)
	}
	if err := database.SQL.QueryRowContext(ctx, `
SELECT status,compatibility_code FROM import_item_core_validations WHERE id=?
`, validationID).Scan(&validationStatus, &validationCode); err != nil ||
		validationStatus != "READY" || validationCode != "READY" {
		t.Fatalf("final validation = %s/%s, error=%v", validationStatus, validationCode, err)
	}
	approved, err := importer.Approve(ctx, itemID, version)
	if err != nil {
		t.Fatal(err)
	}
	contentNames := queryAttachmentStrings(t, database.SQL, `
SELECT file.role||':'||file.logical_name
FROM games game JOIN game_content_files file ON file.game_content_revision_id=game.current_content_revision_id
WHERE game.id=? ORDER BY file.role,file.logical_name
`, approved.GameID)
	if fmt.Sprint(contentNames) != "[COMPANION:b.zip COMPANION:c.zip CONTENT:a.zip]" {
		t.Fatalf("published content = %v", contentNames)
	}
	variantNames := queryAttachmentStrings(t, database.SQL, `
SELECT file.role||':'||file.logical_name
FROM games game JOIN game_variants variant ON variant.game_id=game.id
JOIN variant_files file ON file.game_variant_revision_id=variant.current_revision_id
WHERE game.id=? ORDER BY file.role,file.logical_name
`, approved.GameID)
	if fmt.Sprint(variantNames) != "[PARENT:b.zip PARENT:c.zip]" {
		t.Fatalf("published variant files = %v", variantNames)
	}
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	launcher := launch.New(database.SQL, dependencySet, credentials, time.Now)
	coreID := "fbneo"
	capabilities := launch.Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true}
	pending, err := launcher.Create(ctx, "local", launch.CreateRequest{
		GameID: approved.GameID, CoreID: &coreID, ReturnTo: "/games/" + approved.GameID,
		ClientCapabilities: capabilities,
	})
	if err != nil || pending.Status != "VALIDATION_PENDING" || pending.JobID == "" {
		t.Fatalf("first launch revalidation = %#v, error=%v", pending, err)
	}
	waitParentJob(t, database.SQL, pending.JobID, "SUCCEEDED")
	createdLaunch, err := launcher.Create(ctx, "local", launch.CreateRequest{
		GameID: approved.GameID, CoreID: &coreID, ReturnTo: "/games/" + approved.GameID,
		ClientCapabilities: capabilities,
	})
	if err != nil || createdLaunch.LaunchID == "" {
		t.Fatalf("launch after revalidation = %#v, error=%v", createdLaunch, err)
	}
	configuration, err := launcher.Config(ctx, createdLaunch.LaunchID, createdLaunch.Capability)
	if err != nil || configuration.ParentURL == nil {
		t.Fatalf("launch parent config = %#v, error=%v", configuration.ParentURL, err)
	}
	parentBundle, err := launcher.BundleFiles(ctx, createdLaunch.LaunchID, createdLaunch.Capability, "PARENT")
	if err != nil || fmt.Sprint(parentBundle) == "[]" || len(parentBundle) != 2 ||
		parentBundle[0].LogicalName != "b.zip" || parentBundle[1].LogicalName != "c.zip" {
		t.Fatalf("launch parent bundle = %#v, error=%v", parentBundle, err)
	}
	revalidatedDependencies := queryAttachmentStrings(t, database.SQL, `
SELECT dependency.kind||':'||dependency.logical_archive
FROM game_variants variant
JOIN variant_dependencies dependency ON dependency.game_variant_revision_id=variant.current_revision_id
WHERE variant.game_id=? ORDER BY dependency.kind,dependency.logical_archive
`, approved.GameID)
	if fmt.Sprint(revalidatedDependencies) != "[PARENT:b.zip PARENT:c.zip]" {
		t.Fatalf("revalidated dependencies = %v", revalidatedDependencies)
	}
}

type completedUpload struct{ uploadID, fileID string }

func uploadCompleteFile(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	service *uploads.Service,
	name string,
	contents []byte,
) completedUpload {
	t.Helper()
	upload, err := service.Create(ctx, uploads.CreateRequest{
		SourceType: "FILES", Files: []uploads.FileDeclaration{{ClientFileID: "fixture", RelativePath: name, SizeBytes: int64(len(contents))}},
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	if err := service.PutPart(ctx, upload.ID, upload.Files[0].ID, 0,
		fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)),
		"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	current, err := service.Get(ctx, upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _, err := service.Complete(ctx, upload.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	waitParentJob(t, database, jobID, "SUCCEEDED")
	return completedUpload{uploadID: upload.ID, fileID: upload.Files[0].ID}
}

func insertArcadeParentCatalog(t *testing.T, database *sql.DB) {
	t.Helper()
	now := time.Now().UnixMilli()
	if _, err := database.Exec(`
INSERT INTO core_artifacts(id,core_id,emulatorjs_version,bundle_version,flavor,relative_path,size_bytes,
sha256,provenance_json,compatibility_config_json,enabled,version,created_at_ms,updated_at_ms)
VALUES('attachment-artifact','fbneo','4.2.3','fixture','WASM','data/cores/attachment.data',1,
'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','{}',
'{"schemaVersion":3,"runtimeCoreId":"fbneo","requestedArtifactBasename":"fbneo-wasm.data","canvasResizePolicy":"NONE","defaultOptions":{},"persistentSaveMode":"NONE","persistentSaveKind":null,"inputMode":"STANDARD","startupActions":[],"supportedContentKinds":["SINGLE_FILE"],"multiDisc":null}',1,1,?,?);
INSERT INTO dat_versions(id,core_id,core_artifact_id,source,builtin_relative_path,sha256,parser_version,
compatibility_status,parse_status,is_active,version,created_at_ms,updated_at_ms,parsed_at_ms,activated_at_ms)
VALUES('attachment-dat','fbneo','attachment-artifact','BUILTIN','data/dat/attachment.xml',
'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','fixture',
'MATCHED','READY',1,1,?,?,?,?);
`, now, now, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	relations := []struct{ machine, clone string }{{"a", "b"}, {"b", "c"}, {"c", ""}}
	payloads := map[string][]byte{"a": []byte("child"), "b": []byte("parent"), "c": []byte("root")}
	for ordinal, relation := range relations {
		var clone any
		if relation.clone != "" {
			clone = relation.clone
		}
		payload := payloads[relation.machine]
		sha := sha1.Sum(payload)
		if _, err := database.Exec(`
INSERT INTO dat_machines(dat_version_id,machine_name,description,year,manufacturer,cloneof,romof,
is_explicit_bios,classification) VALUES('attachment-dat',?,'','','',?,NULL,0,'NORMAL')
`, relation.machine, clone); err != nil {
			t.Fatalf("insert relation %d machine: %v", ordinal, err)
		}
		if _, err := database.Exec(`
INSERT INTO dat_rom_entries(dat_version_id,machine_name,ordinal,name,size_bytes,crc32,sha1,status)
VALUES('attachment-dat',?,0,?, ?,?,?,'GOOD')
`, relation.machine, relation.machine+".bin", len(payload),
			fmt.Sprintf("%08x", crc32.ChecksumIEEE(payload)), hex.EncodeToString(sha[:])); err != nil {
			t.Fatalf("insert relation %d: %v", ordinal, err)
		}
	}
}

func arcadeZIP(t *testing.T, name string, contents []byte) []byte {
	t.Helper()
	var result bytes.Buffer
	archive := zip.NewWriter(&result)
	entry, err := archive.Create(name)
	if err == nil {
		_, err = entry.Write(contents)
	}
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func reviewAttachmentInputs(t *testing.T, database *sql.DB, importID string) (string, int64, string, string) {
	t.Helper()
	var itemID, snapshotID, validationID string
	var version int64
	if err := database.QueryRow(`
SELECT item.id,draft.version,draft.effective_source_snapshot_id,validation.id
FROM import_items item JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_core_validations validation ON validation.id=COALESCE(
draft.selected_validation_id,(SELECT candidate.id FROM import_item_core_validations candidate
WHERE candidate.import_item_id=item.id AND candidate.source_snapshot_id=draft.effective_source_snapshot_id
ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1))
WHERE item.import_job_id=?
`, importID).Scan(&itemID, &version, &snapshotID, &validationID); err != nil {
		t.Fatal(err)
	}
	return itemID, version, snapshotID, validationID
}

func waitParentJob(t *testing.T, database *sql.DB, jobID, wanted string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var state string
		if err := database.QueryRow(`SELECT state FROM jobs WHERE id=?`, jobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == wanted {
			return
		}
		if state == "FAILED" || state == "CANCELLED" || time.Now().After(deadline) {
			t.Fatalf("job %s state = %s, want %s", jobID, state, wanted)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func queryAttachmentStrings(t *testing.T, database *sql.DB, query string, arguments ...any) []string {
	t.Helper()
	rows, err := database.Query(query, arguments...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	values := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	return values
}
