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
	"retrom/internal/contentcapability"
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
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	testassert.False(t, err != nil, err)
	runtimeBuilder, err := testsupport.NewRuntimeBuilder(ctx, database.SQL)
	testassert.False(t, err != nil, err)
	service := New(database.SQL, dependencySet, credentials, time.Now).
		WithRuntimeProvider(dependencySet.RuntimeCatalog, runtimeBuilder)
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
		t.Fatalf("create DOS direct launch: %v", err)
	}
	configuration, err := service.Config(ctx, direct.LaunchID, direct.Capability)
	testassert.False(t, err != nil, err)
	directEnvelope := testsupport.RuntimeEnvelope(t, configuration)
	directOptions := testsupport.RuntimeEnvelopeObject(t, directEnvelope, "targetOptions")
	directGame := testsupport.RuntimeEnvelopeResource(t, directEnvelope, "game")
	testassert.Falsef(t, testassert.Any(
		func() bool { return directOptions["dosEntryPath"] != selected },
		func() bool { return directGame["url"] == "" },
		func() bool { return len(testsupport.RuntimeEnvelopeResources(t, directEnvelope, "external")) != 0 },
	), "DOS direct envelope = %#v", directEnvelope)
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
	menuEnvelope := testsupport.RuntimeEnvelope(t, menuConfig)
	menuOptions := testsupport.RuntimeEnvelopeObject(t, menuEnvelope, "targetOptions")
	menuGame := testsupport.RuntimeEnvelopeResource(t, menuEnvelope, "game")
	menuURL, _ := menuGame["url"].(string)
	testassert.Falsef(t, testassert.Any(
		func() bool { return menuOptions["dosEntryPath"] != nil },
		func() bool { return len(testsupport.RuntimeEnvelopeResources(t, menuEnvelope, "external")) != 0 },
		func() bool { return !strings.HasSuffix(menuURL, "game.zip") },
	), "DOS menu envelope = %#v", menuEnvelope)
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
	var variantID, providerID, targetID string
	var gameVersion int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT variant.id,variant.provider_id,variant.target_id,game.version
FROM game_variants variant JOIN games game ON game.id=variant.game_id
WHERE variant.game_id=?
`, approved.GameID).Scan(&variantID, &providerID, &targetID, &gameVersion); err != nil {
		t.Fatal(err)
	}
	transaction, err := database.SQL.BeginTx(ctx, nil)
	testassert.False(t, err != nil, err)
	invalidJobID, _, err := service.queueValidationJob(
		ctx,
		transaction,
		variantID,
		"missing-game",
		gameVersion,
		strings.Repeat("a", 64),
		providerID,
		targetID,
		contentcapability.NewPolicy("SINGLE_FILE"),
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
		"missing-game",
		gameVersion,
		strings.Repeat("a", 64),
		providerID,
		targetID,
		contentcapability.NewPolicy("SINGLE_FILE"),
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
	service.ResumeValidationJob(ctx, invalidJobID)
	var duplicateState string
	var duplicateAttempts int
	if err := database.SQL.QueryRowContext(ctx, `SELECT state,attempt_count FROM jobs WHERE id=?`, invalidJobID).
		Scan(&duplicateState, &duplicateAttempts); err != nil || duplicateState != "RUNNING" || duplicateAttempts != 1 {
		t.Fatalf("duplicate validation resume = %s/%d, error=%v", duplicateState, duplicateAttempts, err)
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
