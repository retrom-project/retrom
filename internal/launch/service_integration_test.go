//go:build integration

package launch

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/libraryimport"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/store"
	"retrom/internal/uploads"
)

func TestDOSDirectBundleIsDeterministicAndInjectsOnlyExactConfig(t *testing.T) {
	t.Parallel()
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var base bytes.Buffer
	writer := zip.NewWriter(&base)
	for _, file := range []struct {
		name string
		body string
	}{{"GAMES/DOOM.EXE", "exe"}, {"GAMES/DOOM.WAD", "wad"}} {
		header := &zip.FileHeader{Name: file.name, Method: zip.Store, ModifiedDate: 33}
		header.SetMode(0o644)
		part, createErr := writer.CreateHeader(header)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := io.WriteString(part, file.body); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	baseMetadata, err := blobs.Put(bytes.NewReader(base.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	service := (&Service{}).WithBlobStore(blobs)
	first, err := service.buildDOSDirectBundle(baseMetadata.SHA256, "GAMES/DOOM.EXE")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.buildDOSDirectBundle(baseMetadata.SHA256, "GAMES/DOOM.EXE")
	if err != nil || first.SHA256 != second.SHA256 {
		t.Fatalf("derived bundle drift = %s/%s, error=%v", first.SHA256, second.SHA256, err)
	}
	contents, err := os.ReadFile(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(contents), int64(len(contents)))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.File) != 3 {
		t.Fatalf("derived entry count = %d", len(archive.File))
	}
	for _, entry := range archive.File {
		if entry.Method != zip.Deflate || len(entry.Extra) != 0 || entry.Mode().Perm() != 0o644 {
			t.Fatalf(
				"derived header = %s method:%d extra:%x mode:%o",
				entry.Name,
				entry.Method,
				entry.Extra,
				entry.Mode().Perm(),
			)
		}
		if entry.Name != "dosbox.conf" {
			continue
		}
		reader, openErr := entry.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		configuration, readErr := io.ReadAll(reader)
		cleanup.Error("close", reader.Close())
		if readErr != nil ||
			string(configuration) != "[autoexec]\r\n@ECHO OFF\r\nC:\r\nCD \"\\GAMES\"\r\n\"DOOM.EXE\"\r\n" {
			t.Fatalf("dosbox.conf = %q, error=%v", configuration, readErr)
		}
	}
	if dosboxConfig("DOOM.EXE") != "[autoexec]\r\n@ECHO OFF\r\nC:\r\nCD \\\r\n\"DOOM.EXE\"\r\n" {
		t.Fatalf("root dosbox.conf = %q", dosboxConfig("DOOM.EXE"))
	}
}

func TestPublishedGameLaunchLocksContentAndCredential(t *testing.T) {
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
	contents := []byte("launchable-gba")
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	upload, err := uploadService.Create(
		ctx,
		uploads.CreateRequest{
			SourceType: "FILES",
			Files: []uploads.FileDeclaration{
				{ClientFileID: "g", RelativePath: "Launch.gba", SizeBytes: int64(len(contents))},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0, "bytes 0-13/14", "sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	current, _ := uploadService.Get(ctx, upload.ID)
	jobID, _, err := uploadService.Complete(ctx, upload.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(3 * time.Second); ; {
		var state string
		_ = database.SQL.QueryRowContext(ctx, "SELECT state FROM jobs WHERE id=?", jobID).Scan(&state)
		if state == "SUCCEEDED" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("finalization = %s", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	importService := libraryimport.New(database.SQL, time.Now)
	createdImport, err := importService.Create(
		ctx,
		libraryimport.CreateRequest{
			UploadID:                 upload.ID,
			TargetPlatformInstanceID: "01980000-0000-7000-8000-000000000005",
			MetadataProvider:         "NONE",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	var itemID string
	if err := database.SQL.QueryRowContext(ctx, "SELECT id FROM import_items WHERE import_job_id=?", createdImport.ImportJobID).Scan(
		&itemID,
	); err != nil {
		t.Fatal(err)
	}
	approved, err := importService.Approve(ctx, itemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	firmwareMetadata, err := blobs.Put(bytes.NewReader([]byte("local-gba-bios")))
	if err != nil {
		t.Fatal(err)
	}
	firmwareBlobID, err := blobstore.EnsureRecord(
		ctx,
		database.SQL,
		firmwareMetadata,
		"application/octet-stream",
		time.Now().UnixMilli(),
	)
	if err != nil {
		t.Fatal(err)
	}
	var requirementID string
	var requirementVersion int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id,
version
FROM bios_requirements
WHERE core_id='mgba'
AND logical_name='gba_bios.bin'
AND enabled=1
`).Scan(&requirementID, &requirementVersion); err != nil {
		t.Fatal(err)
	}
	installationID, _ := uuid.NewV7()
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO bios_installations(id,
requirement_id,
blob_id,
original_filename,
size_bytes,
md5,
sha1,
sha256,
validated_requirement_version,
status,
validation_details_json,
is_active,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
'HASH_WARNING',
'{}',
1,
1,
?,
?)
`,
		installationID.String(),
		requirementID,
		firmwareBlobID,
		"gba_bios.bin",
		firmwareMetadata.Size,
		firmwareMetadata.MD5,
		firmwareMetadata.SHA1,
		firmwareMetadata.SHA256,
		requirementVersion,
		time.Now().UnixMilli(),
		time.Now().UnixMilli(),
	); err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	service := New(database.SQL, dependencySet, credentials, time.Now)
	var fceummArtifactID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id
FROM core_artifacts
WHERE core_id='fceumm'
AND enabled=1
`).Scan(&fceummArtifactID); err != nil {
		t.Fatal(err)
	}
	if status, code := service.validateStaticBIOSForContent(ctx, fceummArtifactID, "Missing.fds"); status != "BLOCKED" || code != "LAUNCH_BIOS_MISSING" {
		t.Fatalf("missing required FDS BIOS validation = %s/%s", status, code)
	}
	createdLaunch, err := service.Create(
		ctx,
		CreateRequest{
			GameID:             approved.GameID,
			ReturnTo:           "/games/" + approved.GameID,
			ClientCapabilities: Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true},
		},
	)
	if err != nil {
		t.Fatalf("create launch: %v", err)
	}
	if _, err := service.Config(ctx, createdLaunch.LaunchID, "bad-capability"); err != ErrCredential {
		t.Fatalf("bad credential error = %v", err)
	}
	configuration, err := service.Config(ctx, createdLaunch.LaunchID, createdLaunch.Capability)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if configuration.Core != "mgba" || configuration.EmulatorJSVersion != "4.2.3" || configuration.GameURL == "" ||
		configuration.BIOSURL == nil || configuration.DefaultCoreOptions["mgba_use_bios"] != "ON" ||
		len(configuration.Warnings) != 1 || configuration.Warnings[0] != "BIOS_HASH_WARNING" {
		t.Fatalf("configuration = %#v", configuration)
	}
	bundle, err := service.BundleFiles(ctx, createdLaunch.LaunchID, createdLaunch.Capability, "BIOS_BUNDLE")
	if err != nil || len(bundle) != 1 || bundle[0].LogicalName != "gba_bios.bin" ||
		bundle[0].SHA256 != firmwareMetadata.SHA256 {
		t.Fatalf("BIOS bundle = %#v, error=%v", bundle, err)
	}
	contentDigest, err := service.ContentBlob(ctx, createdLaunch.LaunchID, createdLaunch.Capability, "Launch.gba")
	if err != nil || contentDigest != base64DigestHex(digest) {
		t.Fatalf("content digest = %s, error = %v", contentDigest, err)
	}
	var lockedVariantRevisionID, lockedArtifactID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT game_variant_revision_id,
core_artifact_id
FROM launch_sessions
WHERE id=?
`, createdLaunch.LaunchID).Scan(&lockedVariantRevisionID, &lockedArtifactID); err != nil {
		t.Fatal(err)
	}
	saveID, _ := uuid.NewV7()
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO save_states(id,
profile_id,
game_id,
game_variant_revision_id,
core_artifact_id,
state_blob_id,
screenshot_blob_id,
name,
active_duration_ms,
version,
created_at_ms,
updated_at_ms) VALUES(?,
'local',
?,
?,
?,
?,
?,
'Locked mGBA save',
0,
1,
?,
?)
`,
		saveID.String(),
		approved.GameID,
		lockedVariantRevisionID,
		lockedArtifactID,
		firmwareBlobID,
		firmwareBlobID,
		time.Now().UnixMilli(),
		time.Now().UnixMilli(),
	); err != nil {
		t.Fatal(err)
	}
	startEvent := PlayEvent{ClientSequence: 0, ClientObservedAtMS: 1_786_000_000_000}
	started, err := service.RecordPlay(ctx, createdLaunch.LaunchID, createdLaunch.Capability, "start", startEvent)
	if err != nil || started.State != "ACTIVE" || started.ClientSequence != 0 {
		t.Fatalf("start play session = %#v, error=%v", started, err)
	}
	replayedStart, err := service.RecordPlay(ctx, createdLaunch.LaunchID, createdLaunch.Capability, "start", startEvent)
	if err != nil || replayedStart.PlaySessionID != started.PlaySessionID {
		t.Fatalf("replayed start = %#v, error=%v", replayedStart, err)
	}
	if _, err := service.RecordPlay(ctx, createdLaunch.LaunchID, createdLaunch.Capability, "start", PlayEvent{ClientSequence: 0, ClientObservedAtMS: startEvent.ClientObservedAtMS + 1}); !errors.Is(
		err,
		ErrBlocked,
	) {
		t.Fatalf("changed start replay error = %v", err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE play_sessions
SET last_heartbeat_at_ms=?
WHERE launch_session_id=?
`, time.Now().Add(-time.Minute).UnixMilli(), createdLaunch.LaunchID); err != nil {
		t.Fatal(err)
	}
	interval := &Interval{Running: true, Visible: true, Paused: false}
	heartbeatEvent := PlayEvent{
		ClientSequence:     1,
		ClientObservedAtMS: startEvent.ClientObservedAtMS + 30_000,
		PreviousInterval:   interval,
	}
	heartbeatResult, err := service.RecordPlay(
		ctx,
		createdLaunch.LaunchID,
		createdLaunch.Capability,
		"heartbeat",
		heartbeatEvent,
	)
	if err != nil || heartbeatResult.AcceptedDuration != int64(45*time.Second/time.Millisecond) {
		t.Fatalf("bounded heartbeat = %#v, error=%v", heartbeatResult, err)
	}
	replayedHeartbeat, err := service.RecordPlay(
		ctx,
		createdLaunch.LaunchID,
		createdLaunch.Capability,
		"heartbeat",
		heartbeatEvent,
	)
	if err != nil || replayedHeartbeat.AcceptedDuration != heartbeatResult.AcceptedDuration {
		t.Fatalf("replayed heartbeat = %#v, error=%v", replayedHeartbeat, err)
	}
	if _, err := service.RecordPlay(ctx, createdLaunch.LaunchID, createdLaunch.Capability, "heartbeat", PlayEvent{ClientSequence: 3, ClientObservedAtMS: startEvent.ClientObservedAtMS + 60_000, PreviousInterval: interval}); !errors.Is(
		err,
		ErrBlocked,
	) {
		t.Fatalf("heartbeat gap error = %v", err)
	}
	finishEvent := PlayEvent{
		ClientSequence:     2,
		ClientObservedAtMS: startEvent.ClientObservedAtMS + 60_000,
		PreviousInterval:   &Interval{Running: false, Visible: true, Paused: false},
	}
	finished, err := service.RecordPlay(ctx, createdLaunch.LaunchID, createdLaunch.Capability, "finish", finishEvent)
	if err != nil || finished.State != "FINISHED" || finished.AcceptedDuration != 0 {
		t.Fatalf("finish = %#v, error=%v", finished, err)
	}
	if replayed, replayErr := service.RecordPlay(ctx, createdLaunch.LaunchID, createdLaunch.Capability, "finish", finishEvent); replayErr != nil ||
		replayed.State != "FINISHED" {
		t.Fatalf("replayed finish = %#v, error=%v", replayed, replayErr)
	}
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE games
SET platform_instance_id='01980000-0000-7000-8000-000000000004',
version=version+1,
updated_at_ms=?
WHERE id=?
`, time.Now().UnixMilli(), approved.GameID); err != nil {
		t.Fatal(err)
	}
	quickLaunch, err := service.Create(
		ctx,
		CreateRequest{
			GameID:             approved.GameID,
			SaveStateID:        ptr(saveID.String()),
			ReturnTo:           "/",
			ClientCapabilities: Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true},
		},
	)
	if err != nil {
		t.Fatalf("locked save quick launch: %v", err)
	}
	quickConfig, err := service.Config(ctx, quickLaunch.LaunchID, quickLaunch.Capability)
	if err != nil || quickConfig.Core != "mgba" || quickConfig.StateURL == nil {
		t.Fatalf("locked save config = %#v, error=%v", quickConfig, err)
	}
	gambatte := "gambatte"
	pending, err := service.Create(
		ctx,
		CreateRequest{
			GameID:             approved.GameID,
			CoreID:             &gambatte,
			ReturnTo:           "/games/" + approved.GameID,
			ClientCapabilities: Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true},
		},
	)
	if err != nil || pending.Status != "VALIDATION_PENDING" || pending.JobID == "" {
		t.Fatalf("pending validation = %#v, error=%v", pending, err)
	}
	var cancellable int
	var dedupeKey, payloadJSON, inputJSON string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT j.cancellable,
j.dedupe_key,
j.payload_json,
s.input_json
FROM jobs j
JOIN job_input_snapshots s ON s.job_id=j.id
AND s.execution_no=j.execution_no
WHERE j.id=?
`, pending.JobID).Scan(&cancellable, &dedupeKey, &payloadJSON, &inputJSON); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	var snapshot validationSnapshot
	if json.Unmarshal([]byte(payloadJSON), &payload) != nil || json.Unmarshal([]byte(inputJSON), &snapshot) != nil ||
		cancellable != 0 ||
		len(dedupeKey) != 64 ||
		dedupeKey == snapshot.Inputs.ValidationInputDigest ||
		payload["inputExecutionNo"] != float64(1) ||
		snapshot.Inputs.GameVariantID == "" {
		t.Fatalf(
			"validation job contract = cancellable:%d dedupe:%s payload:%s snapshot:%s",
			cancellable,
			dedupeKey,
			payloadJSON,
			inputJSON,
		)
	}
	for deadline := time.Now().Add(3 * time.Second); ; {
		var state string
		if err := database.SQL.QueryRowContext(ctx, `
SELECT state
FROM jobs
WHERE id=?
`, pending.JobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" {
			break
		}
		if state == "FAILED" || time.Now().After(deadline) {
			t.Fatalf("variant validation = %s", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var startedAt, deadlineAt int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT execution_started_at_ms,
execution_deadline_at_ms
FROM jobs
WHERE id=?
`, pending.JobID).Scan(&startedAt, &deadlineAt); err != nil ||
		deadlineAt-startedAt != int64(30*time.Minute/time.Millisecond) {
		t.Fatalf("validation deadline = %d..%d, error=%v", startedAt, deadlineAt, err)
	}
	validatedLaunch, err := service.Create(
		ctx,
		CreateRequest{
			GameID:             approved.GameID,
			CoreID:             &gambatte,
			ReturnTo:           "/games/" + approved.GameID,
			ClientCapabilities: Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true},
		},
	)
	if err != nil || validatedLaunch.LaunchID == "" || validatedLaunch.Status != "" {
		t.Fatalf("validated launch = %#v, error=%v", validatedLaunch, err)
	}
	var contentBlobID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT blob_id
FROM game_content_files
WHERE game_content_revision_id=(SELECT current_content_revision_id
FROM games
WHERE id=?)
AND role='CONTENT' LIMIT 1
`, approved.GameID).Scan(&contentBlobID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO variant_files(game_variant_revision_id,
role,
logical_name,
blob_id,
sort_order) VALUES(?,
'PARENT',
'launch.GBA',
?,
0)
`, lockedVariantRevisionID, contentBlobID); err != nil {
		t.Fatal(err)
	}
	mgba := "mgba"
	if _, err := service.Create(ctx, CreateRequest{GameID: approved.GameID, CoreID: &mgba, ReturnTo: "/", ClientCapabilities: Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true}}); !errors.Is(
		err,
		ErrBlocked,
	) {
		t.Fatalf("case-insensitive launch logical-name collision error = %v", err)
	}
}

func ptr(value string) *string { return &value }

func TestDOSLaunchLocksMenuOrSelectedDeterministicBundle(t *testing.T) {
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
	files := []uploads.FileDeclaration{
		{ClientFileID: "exe", RelativePath: "DOOM/DOOM.EXE", SizeBytes: 3},
		{ClientFileID: "wad", RelativePath: "DOOM/DATA.WAD", SizeBytes: 3},
		{ClientFileID: "unsafe", RelativePath: "DOOM/SETUP%.BAT", SizeBytes: 3},
	}
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	upload, err := uploadService.Create(ctx, uploads.CreateRequest{SourceType: "DIRECTORY", Files: files})
	if err != nil {
		t.Fatal(err)
	}
	for index, body := range [][]byte{[]byte("exe"), []byte("wad"), []byte("bat")} {
		digest := sha256.Sum256(body)
		if err := uploadService.PutPart(ctx, upload.ID, upload.Files[index].ID, 0, "bytes 0-2/3", "sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(body)); err != nil {
			t.Fatal(err)
		}
	}
	current, err := uploadService.Get(ctx, upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	finalizeJobID, _, err := uploadService.Complete(ctx, upload.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
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
		if state == "FAILED" || time.Now().After(deadline) {
			t.Fatalf("DOS upload finalize = %s", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	importService := libraryimport.New(database.SQL, time.Now).WithBlobStore(blobs)
	createdImport, err := importService.Create(
		ctx,
		libraryimport.CreateRequest{
			UploadID:                 upload.ID,
			TargetPlatformInstanceID: "01980000-0000-7000-8000-000000000009",
			MetadataProvider:         "NONE",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	var itemID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id
FROM import_items
WHERE import_job_id=?
`, createdImport.ImportJobID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	var defaultPatch libraryimport.DraftPatch
	if err := json.Unmarshal([]byte(`{"defaultDosEntry":null}`), &defaultPatch); err != nil {
		t.Fatal(err)
	}
	patched, err := importService.PatchDraft(ctx, itemID, 1, defaultPatch)
	if err != nil || patched.Version != 2 {
		t.Fatalf("clear default DOS entry = %#v, error=%v", patched, err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	service := New(database.SQL, dependencySet, credentials, time.Now).WithBlobStore(blobs)
	selected := "DOOM/DOOM.EXE"
	capabilities := Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true}
	direct, err := service.Create(
		ctx,
		CreateRequest{
			GameID:             approved.GameID,
			DOSEntry:           &selected,
			ReturnTo:           "/games/" + approved.GameID,
			ClientCapabilities: capabilities,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := service.Config(ctx, direct.LaunchID, direct.Capability)
	if err != nil {
		t.Fatal(err)
	}
	if configuration.DOSEntry != selected || configuration.DefaultCoreOptions["dosbox_pure_conf"] != "inside" {
		t.Fatalf("DOS direct config = %#v", configuration)
	}
	var directFormat, directLogicalName string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT format_version,
logical_name
FROM launch_content_files
WHERE launch_session_id=?
`, direct.LaunchID).Scan(&directFormat, &directLogicalName); err != nil ||
		directFormat != "RETROM_DOS_DIRECT_ZIP_V1" ||
		directLogicalName == "game.zip" {
		t.Fatalf("DOS direct lock = %s/%s, error=%v", directFormat, directLogicalName, err)
	}
	if _, err := service.ContentBlob(ctx, direct.LaunchID, direct.Capability, directLogicalName); err != nil {
		t.Fatalf("DOS direct content: %v", err)
	}
	menu, err := service.Create(
		ctx,
		CreateRequest{GameID: approved.GameID, ReturnTo: "/games/" + approved.GameID, ClientCapabilities: capabilities},
	)
	if err != nil {
		t.Fatal(err)
	}
	menuConfig, err := service.Config(ctx, menu.LaunchID, menu.Capability)
	if err != nil {
		t.Fatal(err)
	}
	if menuConfig.DOSEntry != nil || menuConfig.DefaultCoreOptions["dosbox_pure_conf"] != "" ||
		menuConfig.GameURL[len(menuConfig.GameURL)-len("game.zip"):] != "game.zip" {
		t.Fatalf("DOS menu config = %#v", menuConfig)
	}
	unsafe := "DOOM/SETUP%.BAT"
	if _, err := service.Create(ctx, CreateRequest{GameID: approved.GameID, DOSEntry: &unsafe, ReturnTo: "/", ClientCapabilities: capabilities}); !errors.Is(
		err,
		ErrDOSEntryUnsafe,
	) {
		t.Fatalf("unsafe DOS entry error = %v", err)
	}
	missing := "DOOM/MISSING.EXE"
	if _, err := service.Create(ctx, CreateRequest{GameID: approved.GameID, DOSEntry: &missing, ReturnTo: "/", ClientCapabilities: capabilities}); !errors.Is(
		err,
		ErrDOSEntryMissing,
	) {
		t.Fatalf("missing DOS entry error = %v", err)
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
