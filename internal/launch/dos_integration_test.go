//go:build integration

package launch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
	"retrom/internal/dependencies"
	"retrom/internal/libraryimport"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

func TestDOSLaunchLocksMenuOrSelectedDeterministicBundle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	seedLocalProfile(t, database.SQL)
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(
		filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3",
	)
	testassert.False(t, err != nil, err)
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	files := []uploads.FileDeclaration{
		{ClientFileID: "exe", RelativePath: "DOOM/DOOM.EXE", SizeBytes: 3},
		{ClientFileID: "wad", RelativePath: "DOOM/DATA.WAD", SizeBytes: 3},
		{ClientFileID: "unsafe", RelativePath: "DOOM/SETUP%.BAT", SizeBytes: 3},
	}
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	upload, err := uploadService.Create(ctx, uploads.CreateRequest{SourceType: "DIRECTORY", Files: files})
	testassert.False(t, err != nil, err)
	for index, body := range [][]byte{[]byte("exe"), []byte("wad"), []byte("bat")} {
		digest := sha256.Sum256(body)
		if err := uploadService.PutPart(ctx, upload.ID, upload.Files[index].ID, 0, "bytes 0-2/3", "sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(body)); err != nil {
			t.Fatal(err)
		}
	}
	current, err := uploadService.Get(ctx, upload.ID)
	testassert.False(t, err != nil, err)
	finalizeJobID, _, err := uploadService.Complete(ctx, upload.ID, current.Version)
	testassert.False(t, err != nil, err)
	for deadline := time.Now().Add(3 * time.Second); ; {
		var state string
		_ = database.SQL.QueryRowContext(ctx, `
SELECT state
FROM jobs
WHERE id=?
`, finalizeJobID).Scan(&state)
		if state == "SUCCEEDED" {
			break
		}
		testassert.Falsef(t, testassert.Any(func() bool { return state == "FAILED" }, func() bool { return time.Now().After(deadline) }), "DOS upload finalize = %s", state)
		time.Sleep(10 * time.Millisecond)
	}
	importService := libraryimport.New(database.SQL, time.Now).WithBlobStore(blobs)
	dosID := testsupport.MustPlatformInstanceID(t, database.SQL, "dos/dosbox_pure")
	createdImport, err := importService.Create(
		ctx,
		libraryimport.CreateRequest{
			UploadID:                 upload.ID,
			TargetPlatformInstanceID: dosID,
			MetadataProvider:         "NONE",
		},
	)
	testassert.False(t, err != nil, err)
	var itemID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id
FROM import_items
WHERE import_job_id=?
`, createdImport.ImportJobID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	var defaultPatch libraryimport.DraftPatch
	if err := json.Unmarshal([]byte(`{"defaultDosEntry":null,"tagIds":[]}`), &defaultPatch); err != nil {
		t.Fatal(err)
	}
	patched, err := importService.PatchDraft(ctx, itemID, 1, defaultPatch)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return patched.Version != 2 }), "clear default DOS entry = %#v, error=%v", patched, err)
	var validationCount int
	var selectedDefault sql.NullString
	if err := database.SQL.QueryRowContext(ctx, `
SELECT (SELECT count(*)
FROM import_item_core_validations
WHERE import_item_id=?),
v.default_dos_entry
FROM review_drafts d
JOIN import_item_core_validations v ON v.id=d.selected_validation_id
WHERE d.import_item_id=?
`, itemID, itemID).Scan(&validationCount, &selectedDefault); err != nil ||
		validationCount != 2 ||
		selectedDefault.Valid {
		t.Fatalf("DOS default validation clone = %d/%v, error=%v", validationCount, selectedDefault, err)
	}
	approved, err := importService.Approve(ctx, itemID, 2)
	testassert.False(t, err != nil, err)
	dependencySet, err = dependencies.Load(
		filepath.Join(repositoryRoot, "data"), []string{"4.2.3", "4.3.0-pre"}, "4.2.3",
	)
	testassert.False(t, err != nil, err)
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	testassert.False(t, err != nil, err)
	service := New(database.SQL, dependencySet, credentials, time.Now)
	selected := "DOOM/DOOM.EXE"
	capabilities := Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true}
	var blobCountBefore int
	if err := database.SQL.QueryRowContext(ctx, `SELECT count(*) FROM blobs`).Scan(&blobCountBefore); err != nil {
		t.Fatal(err)
	}
	direct, err := service.Create(
		ctx,
		"local",
		CreateRequest{
			GameID:             approved.GameID,
			DOSEntry:           &selected,
			ReturnTo:           "/games/" + approved.GameID,
			ClientCapabilities: capabilities,
		},
	)
	if err != nil {
		var selectedArtifact, runtimeVersion, contentKind, contentID, variantID, logicalName string
		var selectedCount int
		diagnosticErr := database.SQL.QueryRowContext(ctx, `
SELECT artifact.id,artifact.runtime_version,content.content_kind,content.id,variant.id,
COALESCE((SELECT logical_name FROM game_content_files file
 WHERE file.game_content_revision_id=content.id AND file.role IN ('CONTENT','DISC') LIMIT 1),''),
(SELECT count(*) FROM core_artifacts current
 WHERE current.core_id=artifact.core_id AND current.selected_for_new_bindings=1)
FROM games game
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN game_content_revisions content ON content.id=game.current_content_revision_id
JOIN core_artifacts artifact ON artifact.core_id=instance.default_core_id
JOIN game_variants variant ON variant.game_id=game.id AND variant.core_id=artifact.core_id
WHERE game.id=? AND artifact.selected_for_new_bindings=1
`, approved.GameID).Scan(&selectedArtifact, &runtimeVersion, &contentKind, &contentID, &variantID,
			&logicalName, &selectedCount)
		diagnosticTx, beginErr := database.SQL.BeginTx(ctx, nil)
		if beginErr != nil {
			t.Fatal(beginErr)
		}
		defer cleanup.Rollback(diagnosticTx)
		digest, biosDigest, digestErr := service.validationDigests(
			ctx, diagnosticTx, variantID, contentID, logicalName, contentKind,
			selectedArtifact, sql.NullString{},
		)
		t.Fatalf("create DOS validation launch: %v; artifact=%s runtime=%s kind=%s selected=%d diagnostic=%v digest=%s bios=%s digestErr=%v",
			err, selectedArtifact, runtimeVersion, contentKind, selectedCount, diagnosticErr, digest, biosDigest, digestErr)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return direct.Status != "VALIDATION_PENDING" }, func() bool { return direct.JobID == "" }), "DOS runtime upgrade validation = %#v", direct)
	for deadline := time.Now().Add(3 * time.Second); ; {
		var state string
		var errorCode sql.NullString
		if err := database.SQL.QueryRowContext(ctx, `SELECT state,error_code FROM jobs WHERE id=?`, direct.JobID).
			Scan(&state, &errorCode); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" {
			break
		}
		testassert.Falsef(t, testassert.Any(func() bool { return state == "FAILED" }, func() bool { return time.Now().After(deadline) }), "DOS runtime upgrade validation = %s/%s", state, errorCode.String)
		time.Sleep(10 * time.Millisecond)
	}
	direct, err = service.Create(
		ctx,
		"local",
		CreateRequest{
			GameID:             approved.GameID,
			DOSEntry:           &selected,
			ReturnTo:           "/games/" + approved.GameID,
			ClientCapabilities: capabilities,
		},
	)
	if err != nil {
		var revisionID, artifactID, emulatorVersion, digest, contentID, logicalName string
		var artifactEnabled int
		diagnosticErr := database.SQL.QueryRowContext(ctx, `
		SELECT v.current_revision_id,a.id,a.runtime_version,a.available_for_launch,r.validation_input_digest,r.game_content_revision_id,
COALESCE((SELECT logical_name FROM game_content_files WHERE game_content_revision_id=r.game_content_revision_id AND role='CONTENT' LIMIT 1),'')
FROM game_variants v
JOIN game_variant_revisions r ON r.id=v.current_revision_id
JOIN core_artifacts a ON a.id=r.core_artifact_id
WHERE v.game_id=?
`, approved.GameID).Scan(&revisionID, &artifactID, &emulatorVersion, &artifactEnabled, &digest, &contentID, &logicalName)
		snapshot, biosStatus, biosCode, biosErr := corevalidation.ResolveBIOS(ctx, database.SQL, artifactID, logicalName)
		expectedDigest, digestErr := corevalidation.ValidationInputDigest(artifactID, contentID, sql.NullString{}, snapshot)
		compatibility, compatibilityErr := service.loadArtifactCompatibility(ctx, artifactID)
		var directSafe int
		directErr := database.SQL.QueryRowContext(ctx, `SELECT direct_launch_safe FROM dos_entries WHERE game_content_revision_id=? AND normalized_path=?`, contentID, selected).Scan(&directSafe)
		t.Fatalf("DOS launch after runtime upgrade: %v; revision=%s artifact=%s runtime=%s enabled=%d digest=%s expected=%s content=%q diagnostic=%v bios=%s/%s/%v digestErr=%v compatibility=%#v/%v direct=%d/%v", err, revisionID, artifactID, emulatorVersion, artifactEnabled, digest, expectedDigest, logicalName, diagnosticErr, biosStatus, biosCode, biosErr, digestErr, compatibility, compatibilityErr, directSafe, directErr)
	}
	configuration, err := service.Config(ctx, direct.LaunchID, direct.Capability)
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return configuration.EmulatorJSVersion != "4.3.0-pre" }, func() bool { return configuration.PlayerAdapterID != "ejs-4.3.0-pre-v2" }, func() bool { return configuration.DOSEntry != selected }, func() bool { return configuration.DefaultCoreOptions["dosbox_pure_conf"] != "" }, func() bool { return len(configuration.ExternalFiles) != 0 }), "DOS direct config = %#v", configuration)
	var directFormat, directLogicalName, directBlobID string
	var blobCountAfter int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT format_version,
logical_name,
blob_id
FROM launch_content_files
WHERE launch_session_id=?
`, direct.LaunchID).Scan(&directFormat, &directLogicalName, &directBlobID); err != nil ||
		directFormat != "RETROM_DOS_DIRECT_ZIP_V1" || directLogicalName != "game.zip" {
		t.Fatalf("DOS direct lock = %s/%s/%s, error=%v", directFormat, directLogicalName, directBlobID, err)
	}
	if err := database.SQL.QueryRowContext(ctx, `SELECT count(*) FROM blobs`).Scan(&blobCountAfter); err != nil ||
		blobCountAfter != blobCountBefore {
		t.Fatalf("DOS direct launch materialized blobs = %d -> %d, error=%v", blobCountBefore, blobCountAfter, err)
	}
	locked, err := service.Content(ctx, direct.LaunchID, direct.Capability, directLogicalName)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return locked.Format != "RETROM_DOS_DIRECT_ZIP_V1" }, func() bool { return locked.CoreID != "dosbox_pure" }, func() bool { return locked.DOSEntry == nil }, func() bool { return *locked.DOSEntry != selected }, func() bool { return locked.Digest == "" }), "DOS direct content: %v", err)
	menu, err := service.Create(
		ctx,
		"local",
		CreateRequest{GameID: approved.GameID, ReturnTo: "/games/" + approved.GameID, ClientCapabilities: capabilities},
	)
	testassert.False(t, err != nil, err)
	menuConfig, err := service.Config(ctx, menu.LaunchID, menu.Capability)
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return menuConfig.DOSEntry != nil }, func() bool { return menuConfig.DefaultCoreOptions["dosbox_pure_conf"] != "" }, func() bool { return len(menuConfig.ExternalFiles) != 0 }, func() bool { return menuConfig.GameURL[len(menuConfig.GameURL)-len("game.zip"):] != "game.zip" }), "DOS menu config = %#v", menuConfig)
	unsafe := "DOOM/SETUP%.BAT"
	if _, err := service.Create(ctx, "local", CreateRequest{GameID: approved.GameID, DOSEntry: &unsafe, ReturnTo: "/", ClientCapabilities: capabilities}); !errors.Is(
		err,
		ErrDOSEntryUnsafe,
	) {
		t.Fatalf("unsafe DOS entry error = %v", err)
	}
	missing := "DOOM/MISSING.EXE"
	if _, err := service.Create(ctx, "local", CreateRequest{GameID: approved.GameID, DOSEntry: &missing, ReturnTo: "/", ClientCapabilities: capabilities}); !errors.Is(
		err,
		ErrDOSEntryMissing,
	) {
		t.Fatalf("missing DOS entry error = %v", err)
	}
	var variantID, artifactID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT v.id,r.core_artifact_id
FROM game_variants v
JOIN game_variant_revisions r ON r.id=v.current_revision_id
WHERE v.game_id=?
`, approved.GameID).Scan(&variantID, &artifactID); err != nil {
		t.Fatal(err)
	}
	transaction, err := database.SQL.BeginTx(ctx, nil)
	testassert.False(t, err != nil, err)
	invalidJobID, _, err := service.queueValidationJob(
		ctx,
		transaction,
		variantID,
		"missing-content-revision",
		artifactID,
		sql.NullString{},
		strings.Repeat("0", 64),
		strings.Repeat("0", 64),
	)
	if err != nil {
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	service.ResumeValidationJob(ctx, invalidJobID)
	var failedState string
	var retryable int
	if err := database.SQL.QueryRowContext(ctx, `SELECT state,error_retryable FROM jobs WHERE id=?`, invalidJobID).
		Scan(&failedState, &retryable); err != nil || failedState != "FAILED" || retryable != 1 {
		t.Fatalf("failed validation terminal state = %s/%d, error=%v", failedState, retryable, err)
	}
	retryTx, err := database.SQL.BeginTx(ctx, nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(retryTx)
	retriedJobID, queued, err := service.queueValidationJob(
		ctx,
		retryTx,
		variantID,
		"missing-content-revision",
		artifactID,
		sql.NullString{},
		strings.Repeat("0", 64),
		strings.Repeat("0", 64),
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return !queued }, func() bool { return retriedJobID != invalidJobID }), "automatic validation retry = %s/%t, error=%v", retriedJobID, queued, err)
	if err := retryTx.Commit(); err != nil {
		t.Fatal(err)
	}
	var executionNo, retryEvents int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT execution_no,(SELECT count(*) FROM job_events WHERE job_id=jobs.id AND event_type='RETRY_SCHEDULED'
  AND json_extract(data_json,'$.trigger')='LAUNCH')
FROM jobs WHERE id=? AND state='QUEUED'
`, invalidJobID).Scan(&executionNo, &retryEvents); err != nil || executionNo != 2 || retryEvents != 1 {
		t.Fatalf("automatic validation retry evidence = execution %d/events %d, error=%v", executionNo, retryEvents, err)
	}
	now := time.Now().UnixMilli()
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE jobs
SET state='RUNNING',attempt_count=1,finished_at_ms=NULL,error_code=NULL,error_retryable=NULL,
leased_until_ms=?,updated_at_ms=?
WHERE id=?
`, now-1, now, invalidJobID); err != nil {
		t.Fatal(err)
	}
	service.recoverStaleValidationJobs(ctx)
	if err := database.SQL.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=?`, invalidJobID).
		Scan(&failedState); err != nil || failedState != "QUEUED" {
		t.Fatalf("stale validation recovery = %s, error=%v", failedState, err)
	}
}

func seedLocalProfile(t *testing.T, database *sql.DB) {
	t.Helper()
	if _, err := database.ExecContext(context.Background(), `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('local','Fixture',0)`); err != nil {
		t.Fatal(err)
	}
}

func base64DigestHex(digest [32]byte) string {
	const alphabet = "0123456789abcdef"
	result := make([]byte, 64)
	for index, value := range digest {
		result[index*2] = alphabet[value>>4]
		result[index*2+1] = alphabet[value&15]
	}
	return string(result)
}
