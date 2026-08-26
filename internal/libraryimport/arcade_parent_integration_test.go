//go:build integration

package libraryimport

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"hash/crc32"
	"io"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/launch"
	"retrom/internal/legacychecksum"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

func TestArcadeParentAttachmentsAdvanceImmutableSnapshotsUntilReadyAndPublish(t *testing.T) {
	t.Parallel()
	testArcadeParentAttachmentsAdvanceImmutableSnapshotsUntilReadyAndPublish(t, false)
}

func TestPegasusArcadeParentAttachmentPublishesTheEffectiveReviewSnapshot(t *testing.T) {
	t.Parallel()
	testArcadeParentAttachmentsAdvanceImmutableSnapshotsUntilReadyAndPublish(t, true)
}

func TestReviewBulkApprovalPublishesCurrentArcadeSnapshotV2(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	const (
		profileID = "01990000-0000-7000-8000-00000000c710"
		adminID   = "01990000-0000-7000-8000-00000000c711"
	)
	if _, err := database.SQL.ExecContext(context.Background(),
		`INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,'Arcade Bulk Admin',1)`, profileID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(context.Background(), `
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,?,'arcade.bulk.admin','Arcade Bulk Admin','ADMIN','ENABLED',1,1)
`, adminID, profileID); err != nil {
		t.Fatal(err)
	}
	ctx = authn.WithPrincipal(ctx, authn.Principal{
		UserID: adminID, ProfileID: profileID, Role: "ADMIN",
	})
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	insertArcadeParentCatalog(t, database.SQL)
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	root := uploadCompleteFile(t, ctx, database.SQL, uploadService, "c.zip", arcadeZIP(t, "c.bin", []byte("root")))
	importer := New(database.SQL, time.Now).WithBlobStore(blobs)
	arcadeID := testsupport.MustPlatformInstanceID(t, database.SQL, "arcade/fbneo")
	created, err := importer.Create(ctx, CreateRequest{
		UploadID: root.uploadID, TargetPlatformInstanceID: arcadeID,
		MetadataProvider: "NONE",
	})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return created.ItemCount != 1 }), "arcade import = %#v, error=%v", created, err)
	itemID, _, _, validationID := reviewAttachmentInputs(t, database.SQL, created.ImportJobID)
	var validationStatus, dependencySnapshot string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT status,dependency_snapshot_json FROM import_item_core_validations WHERE id=?
`, validationID).Scan(&validationStatus, &dependencySnapshot); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return validationStatus != "READY" }, func() bool { return !strings.Contains(dependencySnapshot, `"schemaVersion":2`) }), "arcade validation = %s %s", validationStatus, dependencySnapshot)
	preview, err := importer.PreviewReviewBulk(ctx, ReviewBulkScope{ImportJobID: created.ImportJobID})
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return preview.Counts.Matched != 1 }, func() bool { return preview.Counts.StrictReady != 1 }, func() bool { return preview.Counts.NotReadyOrStale != 0 }), "arcade bulk preview = %#v", preview.Counts)
	bulk, err := importer.CreateReviewBulk(ctx, ReviewBulkCreateRequest{
		Scope: preview.Scope, ScopeDigest: preview.ScopeDigest,
		CandidateManifestDigest: preview.CandidateManifestDigest,
	})
	testassert.False(t, err != nil, err)
	deadline := time.Now().Add(5 * time.Second)
	var summary ReviewBulkSummary
	for {
		summary, err = importer.GetReviewBulk(ctx, bulk.BulkApprovalID)
		testassert.False(t, err != nil, err)
		if summary.State == "COMPLETED" || summary.State == "PARTIAL_FAILURE" || summary.State == "FAILED" {
			break
		}
		testassert.Falsef(t, time.Now().After(deadline), "arcade bulk approval did not finish: %#v", summary)
		time.Sleep(10 * time.Millisecond)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return summary.State != "COMPLETED" }, func() bool { return summary.Progress.Published != 1 }, func() bool { return summary.Progress.Processed != 1 }), "arcade bulk result = %#v", summary)
	var gameID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT game_id FROM review_bulk_approval_items
WHERE bulk_approval_id=? AND import_item_id=? AND state='PUBLISHED'
`, bulk.BulkApprovalID, itemID).Scan(&gameID); err != nil || gameID == "" {
		t.Fatalf("arcade bulk game = %q, error=%v", gameID, err)
	}
}

func testArcadeParentAttachmentsAdvanceImmutableSnapshotsUntilReadyAndPublish(t *testing.T, pegasus bool) {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	if _, err := database.SQL.ExecContext(context.Background(), `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('local','Fixture',0)`); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	insertArcadeParentCatalog(t, database.SQL)
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	childZIP := arcadeZIP(t, "a.bin", []byte("child"))
	parentZIP := arcadeZIP(t, "b.bin", []byte("parent"))
	rootZIP := arcadeZIPEntries(t, map[string][]byte{
		"c.bin":           []byte("root"),
		"clone/c-alt.bin": []byte("safe clone-only extra"),
	})
	wrongZIP := arcadeZIP(t, "wrong.bin", []byte("wrong"))
	child := uploadCompleteFile(t, ctx, database.SQL, uploadService, "a.zip", childZIP)
	importer := New(database.SQL, time.Now).WithBlobStore(blobs)
	arcadeID := testsupport.MustPlatformInstanceID(t, database.SQL, "arcade/fbneo")
	created, err := importer.Create(ctx, CreateRequest{
		UploadID: child.uploadID, TargetPlatformInstanceID: arcadeID,
		MetadataProvider: "NONE",
	})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return created.ItemCount != 1 }), "child import = %#v, error=%v", created, err)
	itemID, version, snapshotID, validationID := reviewAttachmentInputs(t, database.SQL, created.ImportJobID)
	if pegasus {
		linkReviewToPegasusOrigin(t, database.SQL, created.ImportJobID, itemID, snapshotID)
	}
	view, found, err := importer.ReviewArcadeDependencies(ctx, itemID)
	testassert.False(t, err != nil, err)
	testassert.True(t, found, "arcade dependencies were not projected")
	viewMap := view.(map[string]any)
	testassert.Falsef(t, testassert.Any(func() bool { return viewMap["machine"] != "a" }, func() bool { return len(viewMap["nodes"].([]map[string]any)) != 2 }), "initial dependency view = %#v", view)
	parent := uploadCompleteFile(t, ctx, database.SQL, uploadService, "anything.zip", parentZIP)
	acceptedB, err := importer.CreateArcadeParentAttachment(ctx, itemID, version, ParentAttachmentRequest{
		ValidationID: validationID, BaseSourceSnapshotID: snapshotID, DependencyMachine: "b",
		UploadFileID: parent.fileID,
	})
	testassert.False(t, err != nil, err)
	waitParentJob(t, database.SQL, acceptedB.JobID, "SUCCEEDED")
	itemID, version, snapshotID, validationID = reviewAttachmentInputs(t, database.SQL, created.ImportJobID)
	testassert.Falsef(t, version != 3, "draft version after b = %d", version)
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
	testassert.Falsef(t, testassert.Any(func() bool { return revision != 2 }, func() bool { return snapshotCount != 2 }, func() bool { return validationStatus != "BLOCKED" }, func() bool { return validationCode != "LAUNCH_PARENT_MISSING" }), "after b = revision:%d count:%d validation:%s/%s", revision, snapshotCount, validationStatus, validationCode)
	wrong := uploadCompleteFile(t, ctx, database.SQL, uploadService, "c.zip", wrongZIP)
	rejectedC, err := importer.CreateArcadeParentAttachment(ctx, itemID, version, ParentAttachmentRequest{
		ValidationID: validationID, BaseSourceSnapshotID: snapshotID, DependencyMachine: "c",
		UploadFileID: wrong.fileID,
	})
	testassert.False(t, err != nil, err)
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
	testassert.Falsef(t, testassert.Any(func() bool { return attachmentState != "REJECTED" }, func() bool { return attachmentCode != ParentErrorMismatch }, func() bool { return currentSnapshotID != snapshotID }), "wrong c = %s/%s snapshot=%s", attachmentState, attachmentCode, currentSnapshotID)
	itemID, version, snapshotID, validationID = reviewAttachmentInputs(t, database.SQL, created.ImportJobID)
	root := uploadCompleteFile(t, ctx, database.SQL, uploadService, "renamed-root.zip", rootZIP)
	acceptedC, err := importer.CreateArcadeParentAttachment(ctx, itemID, version, ParentAttachmentRequest{
		ValidationID: validationID, BaseSourceSnapshotID: snapshotID, DependencyMachine: "c",
		UploadFileID: root.fileID,
	})
	testassert.False(t, err != nil, err)
	waitParentJob(t, database.SQL, acceptedC.JobID, "SUCCEEDED")
	itemID, version, snapshotID, validationID = reviewAttachmentInputs(t, database.SQL, created.ImportJobID)
	testassert.Falsef(t, version != 6, "draft version after c = %d", version)
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
	var acceptedDiagnostics string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT diagnostics_json FROM review_arcade_parent_attachments WHERE id=?
`, acceptedC.AttachmentID).Scan(&acceptedDiagnostics); err != nil ||
		!strings.Contains(acceptedDiagnostics, `"observedRootEntryCount":1`) ||
		!strings.Contains(acceptedDiagnostics, `"ignoredNestedEntryCount":1`) {
		t.Fatalf("merged-style parent diagnostics = %q, error=%v", acceptedDiagnostics, err)
	}
	preview, err := importer.PreviewReviewBulk(ctx, ReviewBulkScope{ImportJobID: created.ImportJobID})
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return preview.Counts.Matched != 1 }, func() bool { return preview.Counts.StrictReady != 1 }, func() bool { return preview.Counts.NotReadyOrStale != 0 }), "arcade parent bulk preview = %#v", preview.Counts)
	approved, err := importer.Approve(ctx, itemID, version)
	testassert.False(t, err != nil, err)
	if pegasus {
		var contentSource, pegasusState string
		if err := database.SQL.QueryRowContext(ctx, `
SELECT content.source_kind,item.execution_state
FROM games game
JOIN game_content_revisions content ON content.id=game.current_content_revision_id
JOIN pegasus_import_items item ON item.published_game_id=game.id
WHERE game.id=?
`, approved.GameID).Scan(&contentSource, &pegasusState); err != nil ||
			contentSource != "SERVER_PEGASUS_IMPORT" || pegasusState != "PUBLISHED" {
			t.Fatalf("Pegasus parent publication = %s/%s, error=%v", contentSource, pegasusState, err)
		}
	}
	contentNames := queryAttachmentStrings(t, database.SQL, `
SELECT file.role||':'||file.logical_name
FROM games game JOIN game_content_files file ON file.game_content_revision_id=game.current_content_revision_id
WHERE game.id=? ORDER BY file.role,file.logical_name
`, approved.GameID)
	testassert.Falsef(t, fmt.Sprint(contentNames) != "[COMPANION:b.zip COMPANION:c.zip CONTENT:a.zip]", "published content = %v", contentNames)
	variantNames := queryAttachmentStrings(t, database.SQL, `
SELECT file.role||':'||file.logical_name
FROM games game JOIN game_variants variant ON variant.game_id=game.id
JOIN variant_files file ON file.game_variant_revision_id=variant.current_revision_id
WHERE game.id=? ORDER BY file.role,file.logical_name
`, approved.GameID)
	testassert.Falsef(t, fmt.Sprint(variantNames) != "[PARENT:b.zip PARENT:c.zip]", "published variant files = %v", variantNames)
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	testassert.False(t, err != nil, err)
	launcher := launch.New(database.SQL, dependencySet, credentials, time.Now)
	coreID := "fbneo"
	capabilities := launch.Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true}
	pending, err := launcher.Create(ctx, "local", launch.CreateRequest{
		GameID: approved.GameID, CoreID: &coreID, ReturnTo: "/games/" + approved.GameID,
		ClientCapabilities: capabilities,
	})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return pending.Status != "VALIDATION_PENDING" }, func() bool { return pending.JobID == "" }), "first launch revalidation = %#v, error=%v", pending, err)
	waitParentJob(t, database.SQL, pending.JobID, "SUCCEEDED")
	createdLaunch, err := launcher.Create(ctx, "local", launch.CreateRequest{
		GameID: approved.GameID, CoreID: &coreID, ReturnTo: "/games/" + approved.GameID,
		ClientCapabilities: capabilities,
	})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return createdLaunch.LaunchID == "" }), "launch after revalidation = %#v, error=%v", createdLaunch, err)
	configuration, err := launcher.Config(ctx, createdLaunch.LaunchID, createdLaunch.Capability)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return configuration.ParentURL == nil }), "launch parent config = %#v, error=%v", configuration.ParentURL, err)
	parentBundle, err := launcher.BundleFiles(ctx, createdLaunch.LaunchID, createdLaunch.Capability, "PARENT")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return fmt.Sprint(parentBundle) == "[]" }, func() bool { return len(parentBundle) != 2 }, func() bool { return parentBundle[0].LogicalName != "b.zip" }, func() bool { return parentBundle[1].LogicalName != "c.zip" }), "launch parent bundle = %#v, error=%v", parentBundle, err)
	revalidatedDependencies := queryAttachmentStrings(t, database.SQL, `
SELECT dependency.kind||':'||dependency.logical_archive
FROM game_variants variant
JOIN variant_dependencies dependency ON dependency.game_variant_revision_id=variant.current_revision_id
WHERE variant.game_id=? ORDER BY dependency.kind,dependency.logical_archive
`, approved.GameID)
	testassert.Falsef(t, fmt.Sprint(revalidatedDependencies) != "[PARENT:b.zip PARENT:c.zip]", "revalidated dependencies = %v", revalidatedDependencies)
}

func linkReviewToPegasusOrigin(
	t *testing.T,
	database *sql.DB,
	importJobID, itemID, sourceSnapshotID string,
) {
	t.Helper()
	var manifestJSON, manifestDigest, contentKind string
	if err := database.QueryRowContext(context.Background(), `
SELECT source_manifest_json,source_manifest_digest,content_kind
FROM import_item_source_snapshots WHERE id=?
`, sourceSnapshotID).Scan(&manifestJSON, &manifestDigest, &contentKind); err != nil {
		t.Fatal(err)
	}
	const userID = "01980000-0000-7000-8000-000000000811"
	const pegasusImportID = "01980000-0000-7000-8000-000000000812"
	const pegasusScanJobID = "01980000-0000-7000-8000-000000000813"
	const pegasusItemID = "01980000-0000-7000-8000-000000000814"
	if _, err := database.ExecContext(context.Background(), `
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'local','pegasus-parent','Pegasus Parent','ADMIN','ENABLED',1,1)
`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(context.Background(), `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,finished_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'SERVER_PEGASUS_SCAN',?,1,'{}',1,'SUCCEEDED',1,4,1,1,2,1,2)
`, pegasusScanJobID, pegasusImportID, strings.Repeat("8", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(context.Background(), `
INSERT INTO pegasus_imports(
  id,root_id,root_label_snapshot,source_relative_path,root_config_digest,state,scan_job_id,
  game_count,processable_item_count,review_pending_item_count,created_by_user_id,
  created_at_ms,updated_at_ms,completed_at_ms,expires_at_ms
) VALUES(?,'games','Games','Arcade',?,'COMPLETED',?,1,1,1,?,1,2,2,999999)
`, pegasusImportID, strings.Repeat("9", 64), pegasusScanJobID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(context.Background(), `
INSERT INTO pegasus_import_items(
  id,import_id,metadata_relative_path,game_ordinal,source_key,title,discovery_state,execution_state,
  content_kind,metadata_json,source_manifest_json,source_manifest_digest,retryable,
  library_import_job_id,library_import_item_id,created_at_ms,updated_at_ms,completed_at_ms
) VALUES(?,?,'Arcade/metadata.pegasus.txt',0,?,'Pegasus Parent','READY','REVIEW_PENDING',
  ?,?, ?,?,0,?,?,1,2,2)
`, pegasusItemID, pegasusImportID, strings.Repeat("a", 64), contentKind,
		`{"title":"Pegasus Parent"}`, manifestJSON, manifestDigest, importJobID, itemID); err != nil {
		t.Fatal(err)
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
	testassert.False(t, err != nil, err)
	digest := sha256.Sum256(contents)
	if err := service.PutPart(ctx, upload.ID, upload.Files[0].ID, 0,
		fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)),
		"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	current, err := service.Get(ctx, upload.ID)
	testassert.False(t, err != nil, err)
	jobID, _, err := service.Complete(ctx, upload.ID, current.Version)
	testassert.False(t, err != nil, err)
	waitParentJob(t, database, jobID, "SUCCEEDED")
	return completedUpload{uploadID: upload.ID, fileID: upload.Files[0].ID}
}

func insertArcadeParentCatalog(t *testing.T, database *sql.DB) {
	t.Helper()
	now := time.Now().UnixMilli()
	if _, err := database.ExecContext(context.Background(), `
INSERT INTO core_artifacts(
 id,core_id,route_key,runtime_family,runtime_adapter_kind,runtime_version,adapter_id,entry_path,
 size_bytes,sha256,manifest_sha256,artifact_set_sha256,requires_threads,save_payload_kind,
 save_max_bytes,provenance_json,compatibility_json,selected_for_new_bindings,available_for_launch,
 version,created_at_ms,updated_at_ms)
VALUES('attachment-artifact','fbneo','DEFAULT','EMULATORJS','EMULATORJS','4.2.3',
'ejs-4.2.3-v3','data/cores/attachment.data',1,
'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
0,'RUNTIME_STATE',67108864,'{}',
'{"schemaVersion":5,"adapterAbi":"emulatorjs-state-v1","runtimeCoreId":"fbneo","requestedArtifactBasename":"fbneo-wasm.data","canvasResizePolicy":"NONE","defaultOptions":{},"inputMode":"STANDARD","startupActions":[],"supportedContentKinds":["SINGLE_FILE"],"multiDisc":null}',
1,1,1,?,?);
INSERT INTO dat_versions(id,core_id,core_artifact_id,builtin_relative_path,sha256,parser_version,
parse_status,is_active,version,created_at_ms,updated_at_ms,parsed_at_ms,activated_at_ms)
VALUES('attachment-dat','fbneo','attachment-artifact','data/dat/attachment.xml',
'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','fixture',
'READY',1,1,?,?,?,?);
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
		_, sha := legacychecksum.Sum(payload)
		if _, err := database.ExecContext(context.Background(), `
INSERT INTO dat_machines(dat_version_id,machine_name,description,year,manufacturer,cloneof,romof,
is_explicit_bios,classification) VALUES('attachment-dat',?,'','','',?,NULL,0,'NORMAL')
`, relation.machine, clone); err != nil {
			t.Fatalf("insert relation %d machine: %v", ordinal, err)
		}
		if _, err := database.ExecContext(context.Background(), `
INSERT INTO dat_rom_entries(dat_version_id,machine_name,ordinal,name,size_bytes,crc32,sha1,status)
VALUES('attachment-dat',?,0,?, ?,?,?,'GOOD')
`, relation.machine, relation.machine+".bin", len(payload),
			fmt.Sprintf("%08x", crc32.ChecksumIEEE(payload)), sha); err != nil {
			t.Fatalf("insert relation %d: %v", ordinal, err)
		}
	}
}

func arcadeZIP(t *testing.T, name string, contents []byte) []byte {
	return arcadeZIPEntries(t, map[string][]byte{name: contents})
}

func arcadeZIPEntries(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var result bytes.Buffer
	archive := zip.NewWriter(&result)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	var err error
	for _, name := range names {
		var entry io.Writer
		entry, err = archive.Create(name)
		if err == nil {
			_, err = entry.Write(entries[name])
		}
		if err != nil {
			break
		}
	}
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	testassert.False(t, err != nil, err)
	return result.Bytes()
}

func reviewAttachmentInputs(t *testing.T, database *sql.DB, importID string) (string, int64, string, string) {
	t.Helper()
	var itemID, snapshotID, validationID string
	var version int64
	if err := database.QueryRowContext(context.Background(), `
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
		var state, kind string
		var errorCode sql.NullString
		if err := database.QueryRowContext(context.Background(),
			`SELECT state,kind,error_code FROM jobs WHERE id=?`, jobID,
		).Scan(&state, &kind, &errorCode); err != nil {
			t.Fatal(err)
		}
		if state == wanted {
			return
		}
		testassert.Falsef(t, testassert.Any(func() bool { return state == "FAILED" }, func() bool { return state == "CANCELLED" }, func() bool { return time.Now().After(deadline) }), "job %s kind/state = %s/%s/%s, want %s", jobID, kind, state, errorCode.String, wanted)
		time.Sleep(10 * time.Millisecond)
	}
}

func queryAttachmentStrings(t *testing.T, database *sql.DB, query string, arguments ...any) []string {
	t.Helper()
	rows, err := database.QueryContext(context.Background(), query, arguments...)
	testassert.False(t, err != nil, err)
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
