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
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
	"retrom/internal/dependencies"
	"retrom/internal/libraryimport"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/store"
	"retrom/internal/uploads"
)

func TestPublishedGameLaunchLocksContentAndCredential(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	seedLocalProfile(t, database.SQL)
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(
		filepath.Join(repositoryRoot, "data"), []string{"4.2.3", "4.3.0-pre"}, "4.2.3",
	)
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
		"local",
		CreateRequest{
			GameID:             approved.GameID,
			ReturnTo:           "/games/" + approved.GameID,
			ClientCapabilities: Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true},
		},
	)
	if err != nil {
		t.Fatalf("create launch: %v", err)
	}
	if createdLaunch.Status == "VALIDATION_PENDING" {
		for deadline := time.Now().Add(3 * time.Second); ; {
			var state string
			var errorCode sql.NullString
			if queryErr := database.SQL.QueryRowContext(ctx, `
SELECT state,error_code FROM jobs WHERE id=?
`, createdLaunch.JobID).Scan(&state, &errorCode); queryErr != nil {
				t.Fatal(queryErr)
			}
			if state == "SUCCEEDED" {
				break
			}
			if state == "FAILED" || time.Now().After(deadline) {
				t.Fatalf("BIOS dependency revalidation = %s/%s", state, errorCode.String)
			}
			time.Sleep(10 * time.Millisecond)
		}
		createdLaunch, err = service.Create(
			ctx,
			"local",
			CreateRequest{
				GameID:             approved.GameID,
				ReturnTo:           "/games/" + approved.GameID,
				ClientCapabilities: Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true},
			},
		)
		if err != nil || createdLaunch.LaunchID == "" {
			t.Fatalf("create launch after BIOS dependency revalidation = %#v, error=%v", createdLaunch, err)
		}
	}
	if _, err := service.Config(ctx, createdLaunch.LaunchID, "bad-capability"); err != ErrCredential {
		t.Fatalf("bad credential error = %v", err)
	}
	configuration, err := service.Config(ctx, createdLaunch.LaunchID, createdLaunch.Capability)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if configuration.Core != "mgba" || configuration.EmulatorJSVersion != "4.2.3" || configuration.GameURL == "" ||
		configuration.CoreName != "mGBA" || configuration.GameTitle != "Launch" || configuration.PlatformName != "Game Boy Advance" ||
		configuration.BIOSURL == nil || configuration.DefaultCoreOptions["mgba_use_bios"] != "ON" ||
		configuration.StartupActions == nil ||
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
		"local",
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
	gbContentID := newUUID()
	contentTx, err := database.SQL.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup.Rollback(contentTx)
	if _, err := contentTx.ExecContext(ctx, `
INSERT INTO game_content_revisions(
  id,game_id,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms
) VALUES(?,?, 'ADMIN_REPLACE','fixture','{}',?,?)
`, gbContentID, approved.GameID, strings.Repeat("f", 64), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := contentTx.ExecContext(ctx, `
INSERT INTO game_content_files(
  game_content_revision_id,role,logical_name,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order
)
SELECT ?,'CONTENT','Launch.gb',blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order
FROM game_content_files
WHERE game_content_revision_id=(
  SELECT game_content_revision_id FROM game_variant_revisions WHERE id=?
) AND role='CONTENT'
`, gbContentID, lockedVariantRevisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := contentTx.ExecContext(ctx, `
UPDATE games
SET current_content_revision_id=?,version=version+1,updated_at_ms=?
WHERE id=?
`, gbContentID, time.Now().UnixMilli(), approved.GameID); err != nil {
		t.Fatal(err)
	}
	if err := contentTx.Commit(); err != nil {
		t.Fatal(err)
	}
	gambatte := "gambatte"
	pending, err := service.Create(
		ctx,
		"local",
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
			var errorCode sql.NullString
			_ = database.SQL.QueryRowContext(ctx, "SELECT error_code FROM jobs WHERE id=?", pending.JobID).
				Scan(&errorCode)
			t.Fatalf("variant validation = %s/%s", state, errorCode.String)
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
		"local",
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
	var currentVariantRevisionID, contentBlobID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT game_variant_revision_id
FROM launch_sessions
WHERE id=?
`, validatedLaunch.LaunchID).Scan(&currentVariantRevisionID); err != nil {
		t.Fatal(err)
	}
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
'launch.GB',
?,
0)
	`, currentVariantRevisionID, contentBlobID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(ctx, "local", CreateRequest{GameID: approved.GameID, CoreID: &gambatte, ReturnTo: "/", ClientCapabilities: Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true}}); !errors.Is(
		err,
		ErrBlocked,
	) {
		t.Fatalf("case-insensitive launch logical-name collision error = %v", err)
	}
}

func TestReviewPreviewStoresFiveSecondScreenshotAndAllowsBlockedRuntimeOverride(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	const actorID = "01980000-0000-7000-8000-000000009995"
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('review-preview-profile','Review Preview Admin',0);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'review-preview-profile','review-preview-admin','Review Preview Admin','ADMIN','ENABLED',0,0);
`, actorID); err != nil {
		t.Fatal(err)
	}
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(
		filepath.Join(repositoryRoot, "data"), []string{"4.2.3", "4.3.0-pre"}, "4.2.3",
	)
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
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	importService := libraryimport.New(database.SQL, time.Now)
	createReview := func(name string, contents []byte, targetID string) string {
		t.Helper()
		upload, createErr := uploadService.Create(ctx, uploads.CreateRequest{
			SourceType: "FILES", Files: []uploads.FileDeclaration{{
				ClientFileID: name, RelativePath: name, SizeBytes: int64(len(contents)),
			}},
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		digest := sha256.Sum256(contents)
		if putErr := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0,
			fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)),
			"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(contents)); putErr != nil {
			t.Fatal(putErr)
		}
		current, getErr := uploadService.Get(ctx, upload.ID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		jobID, _, completeErr := uploadService.Complete(ctx, upload.ID, current.Version)
		if completeErr != nil {
			t.Fatal(completeErr)
		}
		for deadline := time.Now().Add(3 * time.Second); ; {
			var state string
			if queryErr := database.SQL.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=?`, jobID).Scan(&state); queryErr != nil {
				t.Fatal(queryErr)
			}
			if state == "SUCCEEDED" {
				break
			}
			if state == "FAILED" || time.Now().After(deadline) {
				t.Fatalf("review preview upload finalization = %s", state)
			}
			time.Sleep(10 * time.Millisecond)
		}
		created, importErr := importService.Create(ctx, libraryimport.CreateRequest{
			UploadID: upload.ID, TargetPlatformInstanceID: targetID, MetadataProvider: "NONE",
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
		return itemID
	}

	readyItemID := createReview("ready.gba", []byte("review-preview-ready"), "01980000-0000-7000-8000-000000000005")
	blockedItemID := createReview("blocked.fds", []byte("review-preview-blocked"), "01980000-0000-7000-8000-000000000002")
	parentMetadata, err := blobs.Put(bytes.NewReader([]byte("review-preview-parent")))
	if err != nil {
		t.Fatal(err)
	}
	parentBlobID, err := blobstore.EnsureRecord(ctx, database.SQL, parentMetadata, "application/zip", time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	var baseValidationID, sourceSnapshotID, datVersionID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT draft.selected_validation_id,draft.effective_source_snapshot_id,(SELECT id FROM dat_versions ORDER BY id LIMIT 1)
FROM review_drafts draft WHERE draft.import_item_id=?
`, readyItemID).Scan(&baseValidationID, &sourceSnapshotID, &datVersionID); err != nil {
		t.Fatal(err)
	}
	arcadeValidationID := newUUID()
	arcadeSnapshot := fmt.Sprintf(`{"schemaVersion":2,"machine":"review-child","datVersionId":%q,"closure":[],"dependencies":[{"kind":"PARENT","machine":"review-parent","state":"SATISFIED_EXTERNAL","requiredEntries":[]}],"missingEntries":[],"mismatchedEntries":[],"warnings":[]}`, datVersionID)
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO import_item_core_validations(id,import_item_id,target_platform_instance_id,
platform_instance_version,core_id,core_artifact_id,core_artifact_version,prepublish_generation,
dat_version_id,default_dos_entry,source_manifest_digest,source_snapshot_id,prepublish_input_digest,
status,compatibility_code,dependency_snapshot_json,created_at_ms)
SELECT ?,import_item_id,target_platform_instance_id,platform_instance_version,core_id,core_artifact_id,
core_artifact_version,prepublish_generation,?,default_dos_entry,source_manifest_digest,source_snapshot_id,
?,status,compatibility_code,?,created_at_ms+1
FROM import_item_core_validations WHERE id=?
`, arcadeValidationID, datVersionID, strings.Repeat("a", 64), arcadeSnapshot, baseValidationID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO import_item_validation_files(import_item_core_validation_id,role,logical_name,blob_id,sort_order,created_at_ms)
VALUES(?,'PARENT','review-parent.zip',?,0,0)
`, arcadeValidationID, parentBlobID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE review_drafts SET selected_validation_id=?,version=version+1,updated_at_ms=updated_at_ms+1
WHERE import_item_id=? AND effective_source_snapshot_id=?
`, arcadeValidationID, readyItemID, sourceSnapshotID); err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	service := New(database.SQL, dependencySet, credentials, time.Now).WithBlobStore(blobs)
	capabilities := Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true}
	ready, err := service.CreateReviewPreview(ctx, ReviewPreviewRequest{
		ImportItemID: readyItemID, ActorUserID: actorID, IdempotencyKey: "ready-preview-1",
		ClientCapabilities: capabilities,
	})
	if err != nil || !ready.CaptureAllowed || ready.CaptureAfterMS != 5_000 {
		t.Fatalf("ready review preview = %#v, error=%v", ready, err)
	}
	replayed, err := service.CreateReviewPreview(ctx, ReviewPreviewRequest{
		ImportItemID: readyItemID, ActorUserID: actorID, IdempotencyKey: "ready-preview-1",
		ClientCapabilities: capabilities,
	})
	if err != nil || replayed.PreviewID != ready.PreviewID || replayed.Capability != ready.Capability {
		t.Fatalf("replayed review preview = %#v, error=%v", replayed, err)
	}
	configuration, err := service.ReviewPreviewConfig(ctx, ready.PreviewID, ready.Capability)
	if err != nil || configuration.ReviewPreview == nil || !configuration.ReviewPreview.CaptureAllowed ||
		configuration.ReviewPreview.ImportItemID != readyItemID || configuration.PersistentSaveMode != "NONE" ||
		configuration.ParentURL == nil || configuration.StartupActions == nil {
		t.Fatalf("ready review config = %#v, error=%v", configuration, err)
	}
	encodedConfiguration, err := json.Marshal(configuration)
	if err != nil || !bytes.Contains(encodedConfiguration, []byte(`"startupActions":[]`)) {
		t.Fatalf("ready review config JSON = %s, error=%v", encodedConfiguration, err)
	}
	content, err := service.ReviewPreviewContent(ctx, ready.PreviewID, ready.Capability, "ready.gba")
	if err != nil || content.Digest == "" || content.Format != "SOURCE_V1" {
		t.Fatalf("ready review content = %#v, error=%v", content, err)
	}
	pngBody, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	screenshot, err := service.StoreReviewScreenshot(ctx, ready.PreviewID, ready.Capability, bytes.NewReader(pngBody))
	if err != nil || screenshot.ImportItemID != readyItemID || screenshot.WidthPX != 1 || screenshot.HeightPX != 1 {
		t.Fatalf("stored review screenshot = %#v, error=%v", screenshot, err)
	}

	blocked, err := service.CreateReviewPreview(ctx, ReviewPreviewRequest{
		ImportItemID: blockedItemID, ActorUserID: actorID, IdempotencyKey: "blocked-preview-1",
		ClientCapabilities: capabilities,
	})
	if err != nil || !blocked.CaptureAllowed {
		t.Fatalf("blocked best-effort preview = %#v, error=%v", blocked, err)
	}
	blockedConfig, err := service.ReviewPreviewConfig(ctx, blocked.PreviewID, blocked.Capability)
	if err != nil || blockedConfig.ReviewPreview == nil || !blockedConfig.ReviewPreview.CaptureAllowed ||
		blockedConfig.GameURL == "" || blockedConfig.BIOSURL != nil {
		t.Fatalf("blocked best-effort config = %#v, error=%v", blockedConfig, err)
	}
	blockedScreenshot, err := service.StoreReviewScreenshot(
		ctx, blocked.PreviewID, blocked.Capability, bytes.NewReader(pngBody),
	)
	if err != nil || blockedScreenshot.ImportItemID != blockedItemID {
		t.Fatalf("blocked screenshot = %#v, error=%v", blockedScreenshot, err)
	}
	approved, err := importService.Approve(ctx, blockedItemID, 1)
	if err != nil {
		t.Fatalf("approve blocked screenshot override: %v", err)
	}
	var compatibilityCode string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT revision.compatibility_code
FROM games game
JOIN game_variants variant ON variant.game_id=game.id
JOIN game_variant_revisions revision ON revision.id=variant.current_revision_id
WHERE game.id=?
`, approved.GameID).Scan(&compatibilityCode); err != nil || compatibilityCode != "REVIEW_SCREENSHOT_OVERRIDE" {
		t.Fatalf("screenshot override compatibility = %q, error=%v", compatibilityCode, err)
	}
	arcadeOverrideRevisionID := newUUID()
	arcadeOverrideSnapshot := fmt.Sprintf(
		`{"schemaVersion":2,"machine":"review-blocked","datVersionId":%q,"closure":[],"dependencies":[{"kind":"BIOS_OR_BASE","machine":"review-bios","state":"SATISFIED_EXTERNAL","requiredEntries":[]}],"missingEntries":[],"mismatchedEntries":[],"warnings":[]}`,
		datVersionID,
	)
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO game_variant_revisions(id,game_variant_id,game_content_revision_id,core_artifact_id,
dat_version_id,validation_input_digest,emulator_game_id,status,compatibility_code,
dependency_snapshot_json,default_dos_entry,created_at_ms)
SELECT ?,variant.id,current.game_content_revision_id,current.core_artifact_id,?, ?,current.emulator_game_id+100000,
'READY','REVIEW_SCREENSHOT_OVERRIDE',?,current.default_dos_entry,current.created_at_ms+1
FROM game_variants variant
JOIN game_variant_revisions current ON current.id=variant.current_revision_id
WHERE variant.game_id=?
`, arcadeOverrideRevisionID, datVersionID, strings.Repeat("b", 64), arcadeOverrideSnapshot,
		approved.GameID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE game_variants SET current_revision_id=?,version=version+1,updated_at_ms=updated_at_ms+1
WHERE game_id=?
`, arcadeOverrideRevisionID, approved.GameID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO variant_files(game_variant_revision_id,role,logical_name,blob_id,sort_order)
VALUES(?,'BIOS_BUNDLE','review-bios.zip',?,0)
`, arcadeOverrideRevisionID, parentBlobID); err != nil {
		t.Fatal(err)
	}
	createdLaunch, err := service.Create(ctx, "review-preview-profile", CreateRequest{
		GameID: approved.GameID, ReturnTo: "/games/" + approved.GameID, ClientCapabilities: capabilities,
	})
	if err != nil {
		t.Fatalf("launch screenshot-approved game: %v", err)
	}
	publishedConfig, err := service.Config(ctx, createdLaunch.LaunchID, createdLaunch.Capability)
	if err != nil || !slices.Contains(publishedConfig.Warnings, "REVIEW_SCREENSHOT_OVERRIDE") ||
		publishedConfig.BIOSURL == nil {
		t.Fatalf("screenshot-approved config = %#v, error=%v", publishedConfig, err)
	}
	publishedBIOS, err := service.BundleFiles(
		ctx, createdLaunch.LaunchID, createdLaunch.Capability, "BIOS_BUNDLE",
	)
	if err != nil || len(publishedBIOS) != 1 || publishedBIOS[0].LogicalName != "review-bios.zip" ||
		publishedBIOS[0].SHA256 != parentMetadata.SHA256 {
		t.Fatalf("screenshot-approved Arcade BIOS = %#v, error=%v", publishedBIOS, err)
	}
}

func ptr(value string) *string { return &value }

type melondsRequirement struct {
	id          string
	logicalName string
	virtualPath string
	version     int64
	oldDigest   string
	newDigest   string
}

func TestMelonDSExternalBIOSIsLockedPerLaunch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	seedLocalProfile(t, database.SQL)
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
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	var artifactID, platformInstanceID string
	if err := database.SQL.QueryRowContext(ctx, `SELECT id FROM core_artifacts WHERE core_id='melonds' AND enabled=1`).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	if err := database.SQL.QueryRowContext(ctx, `SELECT id FROM platform_instances WHERE platform_id='nds' AND enabled=1`).Scan(&platformInstanceID); err != nil {
		t.Fatal(err)
	}
	requirements := make([]melondsRequirement, 0, 3)
	rows, err := database.SQL.QueryContext(ctx, `
SELECT id,logical_name,emulator_path,version
FROM bios_requirements
WHERE core_artifact_id=? AND delivery_kind='EXTERNAL_FILE' AND enabled=1
ORDER BY logical_name
`, artifactID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var item melondsRequirement
		if err := rows.Scan(&item.id, &item.logicalName, &item.virtualPath, &item.version); err != nil {
			t.Fatal(err)
		}
		requirements = append(requirements, item)
	}
	cleanup.Error("close", rows.Close())
	if err := rows.Err(); err != nil || len(requirements) != 3 {
		t.Fatalf("MelonDS requirements = %#v, error=%v", requirements, err)
	}
	install := func(item *melondsRequirement, generation string, active int) string {
		t.Helper()
		metadata, putErr := blobs.Put(bytes.NewReader([]byte(generation + "-" + item.logicalName)))
		if putErr != nil {
			t.Fatal(putErr)
		}
		blobID, recordErr := blobstore.EnsureRecord(
			ctx,
			database.SQL,
			metadata,
			"application/octet-stream",
			time.Now().UnixMilli(),
		)
		if recordErr != nil {
			t.Fatal(recordErr)
		}
		installationID, _ := uuid.NewV7()
		if _, execErr := database.SQL.ExecContext(ctx, `
INSERT INTO bios_installations(id,requirement_id,blob_id,original_filename,size_bytes,md5,sha1,sha256,
validated_requirement_version,status,validation_details_json,is_active,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,'HASH_WARNING','{}',?,1,?,?)
`, installationID.String(), item.id, blobID, item.logicalName, metadata.Size, metadata.MD5, metadata.SHA1,
			metadata.SHA256, item.version, active, time.Now().UnixMilli(), time.Now().UnixMilli()); execErr != nil {
			t.Fatal(execErr)
		}
		return metadata.SHA256
	}
	for index := range requirements {
		requirements[index].oldDigest = install(&requirements[index], "old", 1)
	}
	gameMetadata, err := blobs.Put(bytes.NewReader([]byte("nds-content")))
	if err != nil {
		t.Fatal(err)
	}
	gameBlobID, err := blobstore.EnsureRecord(ctx, database.SQL, gameMetadata, "application/octet-stream", time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, status, _, err := corevalidation.ResolveBIOS(ctx, database.SQL, artifactID, "game.nds")
	if err != nil || status != "READY" {
		t.Fatalf("MelonDS BIOS snapshot = %#v/%s, error=%v", snapshot, status, err)
	}
	contentID, gameID, metadataID, variantID, revisionID := newUUID(), newUUID(), newUUID(), newUUID(), newUUID()
	digest, err := corevalidation.ValidationInputDigest(artifactID, contentID, sql.NullString{}, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshotJSON, err := snapshot.JSON()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	transaction, err := database.SQL.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup.Rollback(transaction)
	statements := []struct {
		query string
		args  []any
	}{
		{`PRAGMA defer_foreign_keys=ON`, nil},
		{`INSERT INTO game_metadata_revisions(id,game_id,title,description,developer,publisher,genre,players,release_year,source_kind,source_ref_id,created_at_ms)
VALUES(?,?,'MelonDS fixture','','','','',NULL,NULL,'IMPORT_REVIEW','fixture',?)`, []any{metadataID, gameID, now}},
		{`INSERT INTO game_content_revisions(id,game_id,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms)
VALUES(?,?,'IMPORT_REVIEW','fixture','{}',?,?)`, []any{contentID, gameID, strings.Repeat("a", 64), now}},
		{`INSERT INTO games(id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,search_text,version,created_at_ms,updated_at_ms)
VALUES(?,?,'PUBLISHED',?,?,'melonds fixture',1,?,?)`, []any{gameID, platformInstanceID, metadataID, contentID, now, now}},
		{`INSERT INTO game_content_files(game_content_revision_id,role,logical_name,blob_id,sort_order) VALUES(?,'CONTENT','game.nds',?,0)`, []any{contentID, gameBlobID}},
		{`INSERT INTO game_variants(id,game_id,core_id,current_revision_id,version,created_at_ms,updated_at_ms) VALUES(?,?,'melonds',NULL,1,?,?)`, []any{variantID, gameID, now, now}},
		{`INSERT INTO game_variant_revisions(id,game_variant_id,game_content_revision_id,core_artifact_id,dat_version_id,validation_input_digest,emulator_game_id,status,compatibility_code,dependency_snapshot_json,created_at_ms)
VALUES(?,?,?,?,NULL,?,8100,'READY','READY',?,?)`, []any{revisionID, variantID, contentID, artifactID, digest, string(snapshotJSON), now}},
		{`UPDATE game_variants SET current_revision_id=? WHERE id=?`, []any{revisionID, variantID}},
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	service := New(database.SQL, dependencySet, credentials, time.Now)
	capabilities := Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true}
	melonds := "melonds"
	oldLaunch, err := service.Create(ctx, "local", CreateRequest{GameID: gameID, CoreID: &melonds, ReturnTo: "/games/" + gameID, ClientCapabilities: capabilities})
	if err != nil || oldLaunch.LaunchID == "" {
		t.Fatalf("old MelonDS launch = %#v, error=%v", oldLaunch, err)
	}
	assertMelonDSLaunch(t, ctx, service, oldLaunch, requirements, false)
	for index := range requirements {
		if _, err := database.SQL.ExecContext(ctx, `UPDATE bios_installations SET is_active=0,version=version+1,updated_at_ms=? WHERE requirement_id=? AND is_active=1`, time.Now().UnixMilli(), requirements[index].id); err != nil {
			t.Fatal(err)
		}
		requirements[index].newDigest = install(&requirements[index], "new", 1)
	}
	assertMelonDSLaunch(t, ctx, service, oldLaunch, requirements, false)
	pending, err := service.Create(ctx, "local", CreateRequest{GameID: gameID, CoreID: &melonds, ReturnTo: "/games/" + gameID, ClientCapabilities: capabilities})
	if err != nil || pending.Status != "VALIDATION_PENDING" || pending.JobID == "" {
		t.Fatalf("new BIOS validation = %#v, error=%v", pending, err)
	}
	for deadline := time.Now().Add(3 * time.Second); ; {
		var state string
		if err := database.SQL.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=?`, pending.JobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" {
			break
		}
		if state == "FAILED" || time.Now().After(deadline) {
			t.Fatalf("new BIOS validation state = %s", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	newLaunch, err := service.Create(ctx, "local", CreateRequest{GameID: gameID, CoreID: &melonds, ReturnTo: "/games/" + gameID, ClientCapabilities: capabilities})
	if err != nil || newLaunch.LaunchID == "" {
		t.Fatalf("new MelonDS launch = %#v, error=%v", newLaunch, err)
	}
	assertMelonDSLaunch(t, ctx, service, newLaunch, requirements, true)
	if _, err := service.ExternalBlob(ctx, newLaunch.LaunchID, oldLaunch.Capability, requirements[0].logicalName); !errors.Is(err, ErrCredential) {
		t.Fatalf("cross-launch capability error = %v", err)
	}
}

func assertMelonDSLaunch(
	t *testing.T,
	ctx context.Context,
	service *Service,
	launch Created,
	requirements []melondsRequirement,
	useNew bool,
) {
	t.Helper()
	configuration, err := service.Config(ctx, launch.LaunchID, launch.Capability)
	if err != nil || configuration.Core != "melonds" || configuration.RuntimeCore != "melonds" ||
		configuration.InputMode != "POINTER" || len(configuration.ExternalFiles) != 3 {
		t.Fatalf("MelonDS config = %#v, error=%v", configuration, err)
	}
	for _, item := range requirements {
		expectedURL := "/runtime/launches/" + launch.LaunchID + "/external-files/" + item.logicalName
		if configuration.ExternalFiles[item.virtualPath] != expectedURL {
			t.Errorf("external mapping %s = %q", item.virtualPath, configuration.ExternalFiles[item.virtualPath])
		}
		digest, blobErr := service.ExternalBlob(ctx, launch.LaunchID, launch.Capability, item.logicalName)
		expectedDigest := item.oldDigest
		if useNew {
			expectedDigest = item.newDigest
		}
		if blobErr != nil || digest != expectedDigest {
			t.Errorf("external %s = %s, error=%v", item.logicalName, digest, blobErr)
		}
	}
	bundle, err := service.BundleFiles(ctx, launch.LaunchID, launch.Capability, "BIOS_BUNDLE")
	if err != nil || len(bundle) != 0 {
		t.Fatalf("external BIOS leaked into bundle = %#v, error=%v", bundle, err)
	}
}

func TestDOSLaunchLocksMenuOrSelectedDeterministicBundle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	seedLocalProfile(t, database.SQL)
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(
		filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3",
	)
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
	if err := json.Unmarshal([]byte(`{"defaultDosEntry":null,"tagIds":[]}`), &defaultPatch); err != nil {
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
	dependencySet, err = dependencies.Load(
		filepath.Join(repositoryRoot, "data"), []string{"4.2.3", "4.3.0-pre"}, "4.2.3",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
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
		t.Fatal(err)
	}
	if direct.Status != "VALIDATION_PENDING" || direct.JobID == "" {
		t.Fatalf("DOS runtime upgrade validation = %#v", direct)
	}
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
		if state == "FAILED" || time.Now().After(deadline) {
			t.Fatalf("DOS runtime upgrade validation = %s/%s", state, errorCode.String)
		}
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
SELECT v.current_revision_id,a.id,a.emulatorjs_version,a.enabled,r.validation_input_digest,r.game_content_revision_id,
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
	if err != nil {
		t.Fatal(err)
	}
	if configuration.EmulatorJSVersion != "4.3.0-pre" || configuration.PlayerAdapterID != "ejs-4.3.0-pre-v1" ||
		configuration.DOSEntry != selected || configuration.DefaultCoreOptions["dosbox_pure_conf"] != "" ||
		len(configuration.ExternalFiles) != 0 {
		t.Fatalf("DOS direct config = %#v", configuration)
	}
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
	if err != nil || locked.Format != "RETROM_DOS_DIRECT_ZIP_V1" || locked.CoreID != "dosbox_pure" ||
		locked.DOSEntry == nil || *locked.DOSEntry != selected || locked.Digest == "" {
		t.Fatalf("DOS direct content: %v", err)
	}
	menu, err := service.Create(
		ctx,
		"local",
		CreateRequest{GameID: approved.GameID, ReturnTo: "/games/" + approved.GameID, ClientCapabilities: capabilities},
	)
	if err != nil {
		t.Fatal(err)
	}
	menuConfig, err := service.Config(ctx, menu.LaunchID, menu.Capability)
	if err != nil {
		t.Fatal(err)
	}
	if menuConfig.DOSEntry != nil || menuConfig.DefaultCoreOptions["dosbox_pure_conf"] != "" || len(menuConfig.ExternalFiles) != 0 ||
		menuConfig.GameURL[len(menuConfig.GameURL)-len("game.zip"):] != "game.zip" {
		t.Fatalf("DOS menu config = %#v", menuConfig)
	}
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
	if err != nil {
		t.Fatal(err)
	}
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
	if _, err := database.Exec(`INSERT INTO profiles(id,display_name,created_at_ms) VALUES('local','Fixture',0)`); err != nil {
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
