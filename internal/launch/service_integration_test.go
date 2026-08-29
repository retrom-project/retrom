//go:build integration

package launch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/libraryimport"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

func TestPublishedGameLaunchLocksContentAndCredential(t *testing.T) {
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
		filepath.Join(repositoryRoot, "data"), []string{"4.2.3", "4.3.0-pre"}, "4.2.3",
	)
	testassert.False(t, err != nil, err)
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
	testassert.False(t, err != nil, err)
	digest := sha256.Sum256(contents)
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0, "bytes 0-13/14", "sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	current, _ := uploadService.Get(ctx, upload.ID)
	jobID, _, err := uploadService.Complete(ctx, upload.ID, current.Version)
	testassert.False(t, err != nil, err)
	for deadline := time.Now().Add(3 * time.Second); ; {
		var state string
		_ = database.SQL.QueryRowContext(ctx, "SELECT state FROM jobs WHERE id=?", jobID).Scan(&state)
		if state == "SUCCEEDED" {
			break
		}
		testassert.Falsef(t, time.Now().After(deadline), "finalization = %s", state)
		time.Sleep(10 * time.Millisecond)
	}
	importService := libraryimport.New(database.SQL, time.Now)
	gbaID := testsupport.MustPlatformInstanceID(t, database.SQL, "gba/mgba")
	createdImport, err := importService.Create(
		ctx,
		libraryimport.CreateRequest{
			UploadID:                 upload.ID,
			TargetPlatformInstanceID: gbaID,
			MetadataProvider:         "NONE",
		},
	)
	testassert.False(t, err != nil, err)
	var itemID string
	if err := database.SQL.QueryRowContext(ctx, "SELECT id FROM import_items WHERE import_job_id=?", createdImport.ImportJobID).Scan(
		&itemID,
	); err != nil {
		t.Fatal(err)
	}
	approved, err := importService.Approve(ctx, itemID, 1)
	testassert.False(t, err != nil, err)
	firmwareMetadata, err := blobs.Put(bytes.NewReader([]byte("local-gba-bios")))
	testassert.False(t, err != nil, err)
	firmwareBlobID, err := blobstore.EnsureRecord(
		ctx,
		database.SQL,
		firmwareMetadata,
		"application/octet-stream",
		time.Now().UnixMilli(),
	)
	testassert.False(t, err != nil, err)
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
	testassert.False(t, err != nil, err)
	service := New(database.SQL, dependencySet, credentials, time.Now)
	var fceummArtifactID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id
FROM core_artifacts
WHERE core_id='fceumm'
AND selected_for_new_bindings=1
`).Scan(&fceummArtifactID); err != nil {
		t.Fatal(err)
	}
	if status, code := service.validateStaticBIOSForContent(ctx, fceummArtifactID, "Missing.fds"); status != "BLOCKED" || code != "LAUNCH_BIOS_MISSING" {
		t.Fatalf("missing required FDS BIOS validation = %s/%s", status, code)
	}
	assertMissingFDSValidationFinishes(t, ctx, database.SQL, service, approved.GameID)
	createdLaunch, err := service.Create(
		ctx,
		"local",
		CreateRequest{
			GameID:             approved.GameID,
			ReturnTo:           "/games/" + approved.GameID,
			ClientCapabilities: Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true},
		},
	)
	testassert.Falsef(t, err != nil, "create launch: %v", err)
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
			testassert.Falsef(t, testassert.Any(func() bool { return state == "FAILED" }, func() bool { return time.Now().After(deadline) }), "BIOS dependency revalidation = %s/%s", state, errorCode.String)
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
		testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return createdLaunch.LaunchID == "" }), "create launch after BIOS dependency revalidation = %#v, error=%v", createdLaunch, err)
	}
	if _, err := service.Config(ctx, createdLaunch.LaunchID, "bad-capability"); err != ErrCredential {
		t.Fatalf("bad credential error = %v", err)
	}
	configuration, err := service.Config(ctx, createdLaunch.LaunchID, createdLaunch.Capability)
	testassert.Falsef(t, err != nil, "config: %v", err)
	testassert.Falsef(t, testassert.Any(func() bool { return configuration.Core != "mgba" }, func() bool { return configuration.EmulatorJSVersion != "4.2.3" }, func() bool { return configuration.GameURL == "" }, func() bool { return configuration.CoreName != "mGBA" }, func() bool { return configuration.GameTitle != "Launch" }, func() bool { return configuration.PlatformName != "Game Boy Advance" }, func() bool { return configuration.BIOSURL == nil }, func() bool { return configuration.DefaultCoreOptions["mgba_use_bios"] != "ON" }, func() bool { return configuration.StartupActions == nil }, func() bool { return len(configuration.Warnings) != 1 }, func() bool { return configuration.Warnings[0] != "BIOS_HASH_WARNING" }), "configuration = %#v", configuration)
	bundle, err := service.BundleFiles(ctx, createdLaunch.LaunchID, createdLaunch.Capability, "BIOS_BUNDLE")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(bundle) != 1 }, func() bool { return bundle[0].LogicalName != "gba_bios.bin" }, func() bool { return bundle[0].SHA256 != firmwareMetadata.SHA256 }), "BIOS bundle = %#v, error=%v", bundle, err)
	contentDigest, err := service.ContentBlob(ctx, createdLaunch.LaunchID, createdLaunch.Capability, "Launch.gba")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return contentDigest != base64DigestHex(digest) }), "content digest = %s, error = %v", contentDigest, err)
	var lockedContentRevisionID, lockedVariantRevisionID, lockedArtifactID string
	var lockedAdapterABI, lockedDependencyJSON string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT launch.game_content_revision_id,launch.game_variant_revision_id,launch.core_artifact_id,
 json_extract(artifact.compatibility_json,'$.adapterAbi'),revision.dependency_snapshot_json
FROM launch_sessions launch
JOIN core_artifacts artifact ON artifact.id=launch.core_artifact_id
JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
WHERE launch.id=?
`, createdLaunch.LaunchID).Scan(
		&lockedContentRevisionID, &lockedVariantRevisionID, &lockedArtifactID,
		&lockedAdapterABI, &lockedDependencyJSON,
	); err != nil {
		t.Fatal(err)
	}
	lockedDependencyDigest := sha256.Sum256([]byte(lockedDependencyJSON))
	saveID, _ := uuid.NewV7()
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO save_states(id,
profile_id,
game_id,
game_content_revision_id,
game_variant_revision_id,
core_artifact_id,
adapter_abi,
save_abi,
dependency_snapshot_sha256,
payload_blob_id,
payload_kind,
payload_sha256,
payload_size_bytes,
screenshot_blob_id,
source_launch_session_id,
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
?,
?,
?,
'RUNTIME_STATE',
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
		lockedContentRevisionID,
		lockedVariantRevisionID,
		lockedArtifactID,
		lockedAdapterABI,
		lockedAdapterABI,
		hex.EncodeToString(lockedDependencyDigest[:]),
		firmwareBlobID,
		firmwareMetadata.SHA256,
		firmwareMetadata.Size,
		firmwareBlobID,
		createdLaunch.LaunchID,
		time.Now().UnixMilli(),
		time.Now().UnixMilli(),
	); err != nil {
		t.Fatal(err)
	}
	startEvent := PlayEvent{ClientSequence: 0, ClientObservedAtMS: 1_786_000_000_000}
	started, err := service.RecordPlay(ctx, createdLaunch.LaunchID, createdLaunch.Capability, "start", startEvent)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return started.State != "ACTIVE" }, func() bool { return started.ClientSequence != 0 }), "start play session = %#v, error=%v", started, err)
	replayedStart, err := service.RecordPlay(ctx, createdLaunch.LaunchID, createdLaunch.Capability, "start", startEvent)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return replayedStart.PlaySessionID != started.PlaySessionID }), "replayed start = %#v, error=%v", replayedStart, err)
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
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return heartbeatResult.AcceptedDuration != int64(45*time.Second/time.Millisecond) }), "bounded heartbeat = %#v, error=%v", heartbeatResult, err)
	replayedHeartbeat, err := service.RecordPlay(
		ctx,
		createdLaunch.LaunchID,
		createdLaunch.Capability,
		"heartbeat",
		heartbeatEvent,
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return replayedHeartbeat.AcceptedDuration != heartbeatResult.AcceptedDuration }), "replayed heartbeat = %#v, error=%v", replayedHeartbeat, err)
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
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return finished.State != "FINISHED" }, func() bool { return finished.AcceptedDuration != 0 }), "finish = %#v, error=%v", finished, err)
	if replayed, replayErr := service.RecordPlay(ctx, createdLaunch.LaunchID, createdLaunch.Capability, "finish", finishEvent); replayErr != nil ||
		replayed.State != "FINISHED" {
		t.Fatalf("replayed finish = %#v, error=%v", replayed, replayErr)
	}
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE games
SET platform_instance_id=(SELECT id FROM platform_instances WHERE catalog_template_key='gbc/gambatte'),
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
	testassert.Falsef(t, err != nil, "locked save quick launch: %v", err)
	quickConfig, err := service.Config(ctx, quickLaunch.LaunchID, quickLaunch.Capability)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return quickConfig.Core != "mgba" }, func() bool { return quickConfig.StateURL == nil }), "locked save config = %#v, error=%v", quickConfig, err)
	gbContentID := newUUID()
	contentTx, err := database.SQL.BeginTx(ctx, nil)
	testassert.False(t, err != nil, err)
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
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return pending.Status != "VALIDATION_PENDING" }, func() bool { return pending.JobID == "" }), "pending validation = %#v, error=%v", pending, err)
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
	testassert.Falsef(t, testassert.Any(func() bool { return json.Unmarshal([]byte(payloadJSON), &payload) != nil }, func() bool { return json.Unmarshal([]byte(inputJSON), &snapshot) != nil }, func() bool { return cancellable != 0 }, func() bool { return len(dedupeKey) != 64 }, func() bool { return dedupeKey == snapshot.Inputs.ValidationInputDigest }, func() bool { return payload["inputExecutionNo"] != float64(1) }, func() bool { return snapshot.Inputs.GameVariantID == "" }), "validation job contract = cancellable:%d dedupe:%s payload:%s snapshot:%s", cancellable, dedupeKey, payloadJSON, inputJSON)
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
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return validatedLaunch.LaunchID == "" }, func() bool { return validatedLaunch.Status != "" }), "validated launch = %#v, error=%v", validatedLaunch, err)
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

func assertMissingFDSValidationFinishes(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	service *Service,
	sourceGameID string,
) {
	t.Helper()
	const gameID = "60000000-0000-7000-8000-000000000001"
	transaction, err := database.BeginTx(ctx, nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO game_metadata_revisions(id,game_id,title,title_initial,description,developer,publisher,genre,players,release_year,source_kind,source_ref_id,created_at_ms)
SELECT '60000000-0000-7000-8000-000000000002',?,
'Acceptance Missing FDS BIOS','A',description,developer,publisher,genre,players,release_year,source_kind,source_ref_id,?
FROM game_metadata_revisions WHERE game_id=?`, []any{gameID, time.Now().UnixMilli(), sourceGameID}},
		{`INSERT INTO game_content_revisions(id,game_id,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms)
SELECT '60000000-0000-7000-8000-000000000003',?,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,?
FROM game_content_revisions WHERE game_id=?`, []any{gameID, time.Now().UnixMilli(), sourceGameID}},
		{`INSERT INTO games(id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,search_text,version,created_at_ms,updated_at_ms)
SELECT ?,id,'PUBLISHED','60000000-0000-7000-8000-000000000002','60000000-0000-7000-8000-000000000003',
'acceptance missing fds bios',1,?,? FROM platform_instances WHERE catalog_template_key='nes/fceumm'`, []any{gameID, time.Now().UnixMilli(), time.Now().UnixMilli()}},
		{`INSERT INTO game_content_files(game_content_revision_id,role,logical_name,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order)
SELECT '60000000-0000-7000-8000-000000000003',role,
CASE WHEN role='CONTENT' THEN 'Acceptance-Missing-BIOS.fds' ELSE logical_name END,
blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order
FROM game_content_files WHERE game_content_revision_id=(SELECT current_content_revision_id FROM games WHERE id=?)`, []any{sourceGameID}},
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	pending, err := service.Create(ctx, "local", CreateRequest{
		GameID: gameID, ReturnTo: "/games/" + gameID,
		ClientCapabilities: Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true},
	})
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil },
		func() bool { return pending.Status != "VALIDATION_PENDING" },
		func() bool { return pending.JobID == "" },
	), "missing FDS validation = %#v, error=%v", pending, err)
	for deadline := time.Now().Add(3 * time.Second); ; {
		var state string
		var errorCode sql.NullString
		if err := database.QueryRowContext(ctx, `SELECT state,error_code FROM jobs WHERE id=?`, pending.JobID).
			Scan(&state, &errorCode); err != nil {
			t.Fatal(err)
		}
		if state == "FAILED" {
			testassert.Falsef(t, errorCode.String != "LAUNCH_BIOS_MISSING", "missing FDS error = %s", errorCode.String)
			return
		}
		testassert.Falsef(t, testassert.Any(
			func() bool { return state == "SUCCEEDED" },
			func() bool { return time.Now().After(deadline) },
		), "missing FDS validation state = %s/%s", state, errorCode.String)
		time.Sleep(10 * time.Millisecond)
	}
}
