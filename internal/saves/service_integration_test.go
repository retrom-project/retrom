//go:build integration

package saves

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
	"retrom/internal/dependencies"
	"retrom/internal/rpgmaker/checkpoint"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/store"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

type saveFixture struct {
	ctx         context.Context
	database    *store.DB
	blobs       *blobstore.Store
	saves       *Service
	gameID      string
	now         *time.Time
	credentials *retromruntime.Credentials
}

type saveLaunch struct {
	LaunchID   string
	Capability string
}

func newSaveFixture(t *testing.T) *saveFixture {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	now := time.Date(2026, time.August, 6, 2, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), clock)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	if _, err := database.SQL.ExecContext(context.Background(), `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('local','Fixture',0)`); err != nil {
		t.Fatal(err)
	}
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	if err := dependencySet.Bootstrap(ctx, database.SQL, clock()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	content, err := blobs.Put(bytes.NewReader([]byte("save-fixture-gba")))
	testassert.False(t, err != nil, err)
	contentBlobID, err := blobstore.EnsureRecord(
		ctx,
		database.SQL,
		content,
		"application/octet-stream",
		clock().UnixMilli(),
	)
	testassert.False(t, err != nil, err)
	var artifactID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id
FROM core_artifacts
WHERE core_id='mgba'
AND selected_for_new_bindings=1
`).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	gameID := uuid.NewString()
	metadataID := uuid.NewString()
	contentID := uuid.NewString()
	variantID := uuid.NewString()
	variantRevisionID := uuid.NewString()
	dependencySnapshot, status, _, err := corevalidation.ResolveBIOS(ctx, database.SQL, artifactID, "save.gba")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return status != "READY" }), "save fixture dependencies = %#v/%s, error=%v", dependencySnapshot, status, err)
	validationDigest, err := corevalidation.ValidationInputDigest(
		artifactID,
		contentID,
		sql.NullString{},
		dependencySnapshot,
	)
	testassert.False(t, err != nil, err)
	dependencySnapshotJSON, err := dependencySnapshot.JSON()
	testassert.False(t, err != nil, err)
	transaction, err := database.SQL.BeginTx(ctx, nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
PRAGMA defer_foreign_keys=ON
`); err != nil {
		t.Fatal(err)
	}
	stamp := clock().UnixMilli()
	statements := []struct {
		query string
		args  []any
	}{
		{
			`
INSERT INTO game_metadata_revisions(id,
game_id,
title,
title_initial,
description,
developer,
publisher,
genre,
players,
release_year,
source_kind,
source_ref_id,
created_at_ms) VALUES(?,
?,
'Save Fixture',
'S',
'',
'',
'',
'',
NULL,
NULL,
'ADMIN_EDIT',
NULL,
?)
`,
			[]any{metadataID, gameID, stamp},
		},
		{
			`
INSERT INTO game_content_revisions(id,
game_id,
source_kind,
source_ref_id,
source_manifest_json,
source_manifest_digest,
created_at_ms) VALUES(?,
?,
'ADMIN_REPLACE',
'fixture',
'{}',
?,
?)
`,
			[]any{contentID, gameID, strings.Repeat("1", 64), stamp},
		},
		{
			`
INSERT INTO games(id,
platform_instance_id,
status,
current_metadata_revision_id,
current_content_revision_id,
search_text,
version,
created_at_ms,
updated_at_ms) VALUES(?,
(SELECT id FROM platform_instances WHERE catalog_template_key='gba/mgba'),
'PUBLISHED',
?,
?,
'save fixture',
1,
?,
?)
`,
			[]any{gameID, metadataID, contentID, stamp, stamp},
		},
		{
			`
INSERT INTO game_content_files(game_content_revision_id,
role,
logical_name,
blob_id,
source_archive_blob_id,
source_archive_entry_ordinal,
sort_order) VALUES(?,
'CONTENT',
'save.gba',
?,
NULL,
NULL,
0)
`,
			[]any{contentID, contentBlobID},
		},
		{
			`
INSERT INTO game_variants(id,
game_id,
core_id,
current_revision_id,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
'mgba',
NULL,
1,
?,
?)
`,
			[]any{variantID, gameID, stamp, stamp},
		},
		{
			`
INSERT INTO game_variant_revisions(id,
game_variant_id,
game_content_revision_id,
core_artifact_id,
route_key,
dat_version_id,
validation_input_digest,
emulator_game_id,
status,
compatibility_code,
dependency_snapshot_json,
created_at_ms) VALUES(?,
?,
?,
?,
 'DEFAULT',
NULL,
?,
424242,
'READY',
'READY',
?,
?)
`,
			[]any{
				variantRevisionID,
				variantID,
				contentID,
				artifactID,
				validationDigest,
				string(dependencySnapshotJSON),
				stamp,
			},
		},
		{`
UPDATE game_variants
SET current_revision_id=?
WHERE id=?
`, []any{variantRevisionID, variantID}},
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed save fixture: %v", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	testassert.False(t, err != nil, err)
	return &saveFixture{
		ctx:         ctx,
		database:    database,
		blobs:       blobs,
		saves:       New(database.SQL, blobs, credentials, clock),
		gameID:      gameID,
		now:         &now,
		credentials: credentials,
	}
}

func (fixture *saveFixture) createLaunch(t *testing.T) saveLaunch {
	return fixture.createLaunchFromSave(t, nil)
}

func (fixture *saveFixture) createLaunchFromSave(t *testing.T, saveStateID *string) saveLaunch {
	t.Helper()
	launchUUID, err := uuid.NewV7()
	testassert.False(t, err != nil, err)
	capability := fixture.credentials.Capability(launchUUID)
	capabilityHash := retromruntime.HashCapability(capability)
	now := fixture.now.UnixMilli()
	_, err = fixture.database.SQL.ExecContext(fixture.ctx, `
INSERT INTO launch_sessions(
 id,profile_id,purpose,game_id,game_content_revision_id,game_variant_revision_id,
 core_artifact_id,route_key,save_state_id,return_to,credential_sha256,state,bootstrap_expires_at_ms,
 idle_expires_at_ms,activated_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms)
SELECT ?, 'local','PRODUCT',game.id,game.current_content_revision_id,revision.id,
 revision.core_artifact_id,revision.route_key,?,?,?,'ACTIVE',?,?,?, ?,?,?
FROM games game
JOIN game_variants variant ON variant.game_id=game.id
JOIN game_variant_revisions revision ON revision.id=variant.current_revision_id
WHERE game.id=?
	`, launchUUID.String(), saveStateID, "/games/"+fixture.gameID, capabilityHash[:],
		now+300_000, now+120_000, now, now+28_800_000, now, now, fixture.gameID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.database.SQL.ExecContext(fixture.ctx, `
INSERT INTO launch_content_files(launch_session_id,logical_name,blob_id,format_version,created_at_ms)
SELECT ?,file.logical_name,file.blob_id,'SOURCE_V1',?
FROM games game JOIN game_content_files file
 ON file.game_content_revision_id=game.current_content_revision_id AND file.role='CONTENT'
WHERE game.id=?
`, launchUUID.String(), now, fixture.gameID)
	if err != nil {
		t.Fatal(err)
	}
	return saveLaunch{
		LaunchID: launchUUID.String(), Capability: retromruntime.EncodeCapability(capability),
	}
}

func manualRequest(t *testing.T, name string, state, screenshot []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	metadataHeader := makeTextHeader("metadata", "metadata.json", "application/json")
	metadata, err := writer.CreatePart(metadataHeader)
	testassert.False(t, err != nil, err)
	_, _ = metadata.Write([]byte(`{"payloadKind":"RUNTIME_STATE","name":"` + name + `"}`))
	statePart, err := writer.CreateFormFile("payload", "state.bin")
	testassert.False(t, err != nil, err)
	_, _ = statePart.Write(state)
	if screenshot != nil {
		screenshotHeader := makeTextHeader("screenshot", "screenshot.png", "image/png")
		screenshotPart, err := writer.CreatePart(screenshotHeader)
		testassert.False(t, err != nil, err)
		_, _ = screenshotPart.Write(screenshot)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "/", &body)
	testassert.False(t, err != nil, err)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func validationRequest(t *testing.T, payload []byte) *http.Request {
	return validationRequestMetadata(t, `{"payloadKind":"NATIVE_SAVE_BUNDLE_V1"}`, payload)
}

func validationRequestMetadata(t *testing.T, metadataJSON string, payload []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	metadata, err := writer.CreatePart(makeTextHeader("metadata", "metadata.json", "application/json"))
	testassert.False(t, err != nil, err)
	_, _ = metadata.Write([]byte(metadataJSON))
	payloadPart, err := writer.CreateFormFile("payload", "checkpoint.bin")
	testassert.False(t, err != nil, err)
	_, _ = payloadPart.Write(payload)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "/", &body)
	testassert.False(t, err != nil, err)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func makeTextHeader(name, filename, mediaType string) textproto.MIMEHeader {
	return textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="` + name + `"; filename="` + filename + `"`},
		"Content-Type":        {mediaType},
	}
}

func screenshotPNG(t *testing.T) []byte {
	t.Helper()
	value := image.NewRGBA(image.Rect(0, 0, 2, 2))
	value.Set(0, 0, color.RGBA{R: 0x68, G: 0x55, B: 0xd9, A: 0xff})
	var result bytes.Buffer
	if err := png.Encode(&result, value); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func TestManualStateRequiresAtomicNonEmptyStateAndScreenshot(t *testing.T) {
	fixture := newSaveFixture(t)
	created := fixture.createLaunch(t)
	key := uuid.NewString()
	state := []byte("manual-state")
	screenshot := screenshotPNG(t)
	result, replayed, err := fixture.saves.CreateManual(
		fixture.ctx,
		created.LaunchID,
		created.Capability,
		key,
		manualRequest(t, "存档一", state, screenshot),
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return replayed }, func() bool { return result.SaveStateID == "" }), "manual state = %#v, replayed=%v, error=%v", result, replayed, err)
	var sourceLaunchID string
	var stateSize, screenshotSize int64
	if err := fixture.database.SQL.QueryRowContext(fixture.ctx, `
SELECT s.source_launch_session_id,
state_blob.size_bytes,
screenshot_blob.size_bytes
FROM save_states s
JOIN blobs state_blob ON state_blob.id=s.payload_blob_id
JOIN blobs screenshot_blob ON screenshot_blob.id=s.screenshot_blob_id
WHERE s.id=?
`, result.SaveStateID).Scan(&sourceLaunchID, &stateSize, &screenshotSize); err != nil ||
		sourceLaunchID != created.LaunchID ||
		stateSize != int64(len(state)) ||
		screenshotSize != int64(len(screenshot)) {
		t.Fatalf("manual source/blob references = %s/%d/%d, error=%v", sourceLaunchID, stateSize, screenshotSize, err)
	}
	replay, replayed, err := fixture.saves.CreateManual(
		fixture.ctx,
		created.LaunchID,
		created.Capability,
		key,
		manualRequest(t, "存档一", state, screenshot),
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return !replayed }, func() bool { return replay.SaveStateID != result.SaveStateID }), "manual replay = %#v, replayed=%v, error=%v", replay, replayed, err)
	if _, _, err := fixture.saves.CreateManual(fixture.ctx, created.LaunchID, created.Capability, uuid.NewString(), manualRequest(t, "空状态", nil, screenshot)); !errors.Is(
		err,
		ErrCheckpointInvalid,
	) {
		t.Fatalf("empty state error = %v", err)
	}
	var count int
	if err := fixture.database.SQL.QueryRowContext(fixture.ctx, `
SELECT count(*)
FROM save_states
`).Scan(&count); err != nil ||
		count != 1 {
		t.Fatalf("save count after invalid request = %d, error=%v", count, err)
	}
}

func TestProductCheckpointAllowsOptionalScreenshotAndRestoresExactBinding(t *testing.T) {
	fixture := newSaveFixture(t)
	original := fixture.createLaunch(t)
	payload := []byte("cross-launch-product-state")
	result, replayed, err := fixture.saves.CreateManual(
		fixture.ctx,
		original.LaunchID,
		original.Capability,
		uuid.NewString(),
		manualRequest(t, "无截图存档", payload, nil),
	)
	if err != nil || replayed || result.SaveStateID == "" || result.ScreenshotURL != nil ||
		result.PayloadKind != "RUNTIME_STATE" || result.ResourceKind != "SAVE_STATE" {
		t.Fatalf("created=%#v replayed=%v error=%v", result, replayed, err)
	}
	now := fixture.now.UnixMilli()
	if _, err := fixture.database.SQL.ExecContext(fixture.ctx, `
UPDATE launch_sessions SET state='FINISHED',finished_at_ms=?,updated_at_ms=?,version=version+1
WHERE id=?
`, now, now, original.LaunchID); err != nil {
		t.Fatal(err)
	}
	restored := fixture.createLaunchFromSave(t, &result.SaveStateID)
	digest, err := fixture.saves.StateDigest(fixture.ctx, restored.LaunchID, restored.Capability)
	expectedDigest := sha256.Sum256(payload)
	if err != nil || digest != hex.EncodeToString(expectedDigest[:]) {
		t.Fatalf("restore digest=%s error=%v", digest, err)
	}
	if _, err := fixture.database.SQL.ExecContext(fixture.ctx, `
DROP TRIGGER save_states_payload_immutable
`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.SQL.ExecContext(fixture.ctx, `
UPDATE save_states SET adapter_abi='drifted-adapter' WHERE id=?
`, result.SaveStateID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.saves.StateDigest(
		fixture.ctx, restored.LaunchID, restored.Capability,
	); !errors.Is(err, ErrCheckpointIncompatible) {
		t.Fatalf("binding drift error=%v", err)
	}
}

func TestValidationCheckpointIsTemporaryAndNeverCreatesProductSave(t *testing.T) {
	fixture := newSaveFixture(t)
	created, validationID := fixture.createValidationLaunch(t)
	payload, err := checkpoint.Encode(checkpoint.EngineRPG2000, 100, []checkpoint.Entry{{
		Store: checkpoint.StoreFilesystem, Key: "Save100.lsd",
		MediaType: "application/octet-stream", Data: []byte("saved-map-b"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	expectedDigest := sha256.Sum256(payload)
	expectedSHA256 := hex.EncodeToString(expectedDigest[:])
	if _, _, err := fixture.saves.CreateManual(
		fixture.ctx, created.LaunchID, created.Capability, uuid.NewString(),
		validationRequestMetadata(t, `{"payloadKind":"NATIVE_SAVE_BUNDLE_V1","name":""}`, payload),
	); !errors.Is(err, ErrInvalid) {
		t.Fatalf("validation metadata with name error=%v", err)
	}
	if _, _, err := fixture.saves.CreateManual(
		fixture.ctx, created.LaunchID, created.Capability, uuid.NewString(),
		validationRequestMetadata(t,
			`{"payloadKind":"NATIVE_SAVE_BUNDLE_V1","payloadKind":"NATIVE_SAVE_BUNDLE_V1"}`, payload),
	); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate metadata key error=%v", err)
	}
	checkpointKey := uuid.NewString()
	result, replayed, err := fixture.saves.CreateManual(
		fixture.ctx, created.LaunchID, created.Capability, checkpointKey, validationRequest(t, payload),
	)
	if err != nil || replayed || result.ResourceKind != "RPG_RUNTIME_VALIDATION_CHECKPOINT" ||
		result.ValidationID != validationID || result.SaveStateID != "" || result.NativeProfile == nil ||
		*result.NativeProfile != "EASYRPG_V1" || result.ResumeSlot == nil || *result.ResumeSlot != 100 ||
		result.SizeBytes != int64(len(payload)) || result.PayloadSHA256 != expectedSHA256 {
		t.Fatalf("validation checkpoint=%#v replayed=%v error=%v", result, replayed, err)
	}
	var checkpointCount, saveCount int
	if err := fixture.database.SQL.QueryRowContext(fixture.ctx, `
SELECT (SELECT count(*) FROM rpgmaker_runtime_validation_checkpoints WHERE validation_id=?),
       (SELECT count(*) FROM save_states)
`, validationID).Scan(&checkpointCount, &saveCount); err != nil {
		t.Fatal(err)
	}
	if checkpointCount != 1 || saveCount != 0 {
		t.Fatalf("checkpoint/save counts=%d/%d", checkpointCount, saveCount)
	}
	replayedResult, replayed, err := fixture.saves.CreateManual(
		fixture.ctx, created.LaunchID, created.Capability, checkpointKey, validationRequest(t, payload),
	)
	if err != nil || !replayed || replayedResult.ValidationID != validationID ||
		replayedResult.ResourceKind != "RPG_RUNTIME_VALIDATION_CHECKPOINT" ||
		replayedResult.SizeBytes != int64(len(payload)) || replayedResult.PayloadSHA256 != expectedSHA256 {
		t.Fatalf("validation replay=%#v replayed=%v error=%v", replayedResult, replayed, err)
	}
	status, err := fixture.saves.CheckpointStatus(fixture.ctx, created.LaunchID, created.Capability)
	if err != nil || status.Availability.Available || status.Availability.Reason == nil ||
		*status.Availability.Reason != "CHECKPOINT_ALREADY_CREATED" {
		t.Fatalf("checkpoint status=%#v error=%v", status, err)
	}
	if _, _, err := fixture.saves.CreateManual(
		fixture.ctx, created.LaunchID, created.Capability, uuid.NewString(), validationRequest(t, payload),
	); !errors.Is(err, ErrCheckpointUnavailable) {
		t.Fatalf("second checkpoint error=%v", err)
	}
	now := fixture.now.UnixMilli()
	appendSaveValidationOriginalGates(t, fixture.database.SQL, validationID, created.LaunchID, now)
	mustSaveSQL(t, fixture.database.SQL, `
UPDATE rpgmaker_runtime_validations SET state='CHECKPOINTED',updated_at_ms=? WHERE id=?`, now, validationID)
	mustSaveSQL(t, fixture.database.SQL, `
UPDATE launch_sessions SET state='FINISHED',finished_at_ms=?,updated_at_ms=?,version=version+1 WHERE id=?`,
		now, now, created.LaunchID)
	restore := fixture.createValidationRestoreLaunch(t, validationID)
	digest, err := fixture.saves.StateDigest(fixture.ctx, restore.LaunchID, restore.Capability)
	if err != nil || restore.LaunchID == created.LaunchID || digest != hex.EncodeToString(expectedDigest[:]) {
		t.Fatalf("validation restore launch=%s original=%s digest=%s error=%v",
			restore.LaunchID, created.LaunchID, digest, err)
	}
}

func appendSaveValidationOriginalGates(
	t *testing.T,
	database *sql.DB,
	validationID, launchID string,
	now int64,
) {
	t.Helper()
	gates := []string{
		"RUNTIME_READY", "ENGINE_PROFILE", "FRAMES_300", "INPUT", "AUDIO",
		"INITIAL_POSITION_RECORDED", "SAVE_POINT_RECORDED", "CHECKPOINT_CREATED",
		"POST_SAVE_STATE_DIVERGED", "ORIGINAL_LAUNCH_ENDED",
	}
	sequence := int64(1)
	for _, gate := range gates {
		for _, phase := range []string{"BEGIN", "PASS"} {
			mustSaveSQL(t, database, `
INSERT INTO rpgmaker_runtime_validation_gate_events(
 validation_id,sequence,event_id,launch_id,gate,phase,observed_at_ms,evidence_json,created_at_ms)
VALUES(?,?,?,?,?,?,?,?,?)`, validationID, sequence, uuid.NewString(), launchID, gate, phase,
				now+sequence, saveValidationGateEvidence(gate, phase), now+sequence)
			mustSaveSQL(t, database, `
UPDATE rpgmaker_runtime_validations SET last_gate_sequence=?,updated_at_ms=? WHERE id=?`,
				sequence, now+sequence, validationID)
			sequence++
		}
	}
}

func saveValidationGateEvidence(gate, phase string) string {
	if phase != "PASS" {
		return "{}"
	}
	switch gate {
	case "INITIAL_POSITION_RECORDED":
		return `{"mapId":1,"playerX":1,"playerY":1,"fixtureState":0}`
	case "SAVE_POINT_RECORDED":
		return `{"mapId":1,"playerX":2,"playerY":1,"fixtureState":1}`
	case "POST_SAVE_STATE_DIVERGED":
		return `{"mapId":1,"playerX":3,"playerY":1,"fixtureState":2}`
	default:
		return "{}"
	}
}

func (fixture *saveFixture) createValidationLaunch(t *testing.T) (saveLaunch, string) {
	t.Helper()
	now := fixture.now.UnixMilli()
	validationID := uuid.NewString()
	launchID := uuid.NewString()
	artifactID := uuid.NewString()
	capabilityUUID := uuid.MustParse(launchID)
	capability := fixture.credentials.Capability(capabilityUUID)
	capabilityHash := retromruntime.HashCapability(capability)
	userID := uuid.NewString()
	ids := map[string]string{
		"artifact": artifactID, "directory": uuid.NewString(), "upload": uuid.NewString(),
		"import": uuid.NewString(), "item": uuid.NewString(), "snapshot": uuid.NewString(),
		"review": uuid.NewString(), "validation": validationID,
	}
	mustSaveSQL(t, fixture.database.SQL, `
UPDATE core_artifacts
SET selected_for_new_bindings=0,version=version+1,updated_at_ms=?
WHERE core_id='rpgmaker_2000' AND selected_for_new_bindings=1`, now)
	mustSaveSQL(t, fixture.database.SQL, `
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'local',?,'Save Admin','ADMIN','ENABLED',?,?)`, userID, "save-admin-"+userID[:8], now, now)
	mustSaveSQL(t, fixture.database.SQL, `
INSERT INTO core_artifacts(
 id,core_id,route_key,runtime_family,runtime_adapter_kind,runtime_version,adapter_id,entry_path,
 size_bytes,sha256,manifest_sha256,artifact_set_sha256,requires_threads,save_payload_kind,
 save_max_bytes,provenance_json,compatibility_json,selected_for_new_bindings,available_for_launch,
 version,created_at_ms,updated_at_ms)
VALUES(?,'rpgmaker_2000','RPG2000_EASYRPG_SAVE_TEST','RPGMAKER','EASYRPG_WEB','test',
 'easyrpg-web-v1','runtime/easyrpg.js',1,?,?,?,0,'NATIVE_SAVE_BUNDLE_V1',67108864,
 '{}','{"adapterAbi":"easy-save-abi"}',1,1,1,?,?)`, artifactID, strings.Repeat("a", 64), strings.Repeat("b", 64),
		strings.Repeat("c", 64), now, now)
	mustSaveSQL(t, fixture.database.SQL, `
INSERT INTO platform_instances(
 id,platform_id,default_core_id,name,slug,sort_order,enabled,version,created_at_ms,updated_at_ms,catalog_template_key)
VALUES(?,'rpgmaker','rpgmaker_2000','RPG Maker 2000 Save','rpg-maker-save-test',999,1,1,?,?,NULL)`,
		ids["directory"], now, now)
	mustSaveSQL(t, fixture.database.SQL, `
INSERT INTO upload_sessions(
 id,purpose,state,source_type,total_files,total_bytes,manifest_digest,expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'RPG_MAKER_PROJECT','COMPLETE','DIRECTORY',1,10,?,?,?,?)`, ids["upload"],
		strings.Repeat("d", 64), now+1_000_000, now, now)
	mustSaveSQL(t, fixture.database.SQL, `
INSERT INTO import_jobs(
 id,upload_session_id,target_platform_instance_id,platform_instance_version,platform_id,default_core_id,
 core_artifact_id,metadata_provider,config_snapshot_json,config_snapshot_digest,state,total_item_count,
 review_pending_item_count,created_at_ms,updated_at_ms)
VALUES(?,?,?,1,'rpgmaker','rpgmaker_2000',?,'NONE','{}',?,'REVIEW_PENDING',1,1,?,?)`,
		ids["import"], ids["upload"], ids["directory"], artifactID, strings.Repeat("e", 64), now, now)
	mustSaveSQL(t, fixture.database.SQL, `
INSERT INTO import_items(
 id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,search_text,created_at_ms,updated_at_ms)
VALUES(?,?,?,'REVIEW_PENDING','{}',?,'save validation fixture',?,?)`, ids["item"], ids["import"],
		strings.Repeat("1", 64), strings.Repeat("2", 64), now, now)
	manifest := `{"schemaVersion":2,"contentKind":"RPG_MAKER_PROJECT_V1","fileCount":1,"totalBytes":10,"filesDigest":"` +
		strings.Repeat("3", 64) + `"}`
	mustSaveSQL(t, fixture.database.SQL, `
INSERT INTO import_item_source_snapshots(
 id,import_item_id,revision_no,content_kind,source_manifest_json,source_manifest_digest,created_by,created_at_ms)
VALUES(?,?,1,'RPG_MAKER_PROJECT_V1',?,?,'IDENTIFICATION',?)`, ids["snapshot"], ids["item"], manifest,
		strings.Repeat("4", 64), now)
	mustSaveSQL(t, fixture.database.SQL, `
INSERT INTO review_drafts(
 id,import_item_id,target_platform_instance_id,metadata_json,runtime_binding_revision,version,
 created_at_ms,updated_at_ms,effective_source_snapshot_id)
VALUES(?,?,?,'{}',1,1,?,?,?)`, ids["review"], ids["item"], ids["directory"], now, now, ids["snapshot"])
	mustSaveSQL(t, fixture.database.SQL, `
INSERT INTO rpgmaker_review_profiles(
 review_draft_id,selected_core_id,generation,evidence_family,evidence_generation,evidence_confidence,
 file_count,total_bytes,project_fingerprint,requirements_sha256,analysis_json,self_contained_override,
 route_key,artifact_id,artifact_set_sha256,adapter_id,adapter_abi,dependency_snapshot_sha256,
 created_at_ms,updated_at_ms)
VALUES(?,'rpgmaker_2000','RPG2000','RPG2K',NULL,'FAMILY_ONLY',1,10,?,?,'{}',1,
 'RPG2000_EASYRPG_SAVE_TEST',?,?,'easyrpg-web-v1','easy-save-abi',?,?,?)`, ids["review"],
		strings.Repeat("5", 64), strings.Repeat("6", 64), artifactID, strings.Repeat("c", 64),
		strings.Repeat("7", 64), now, now)
	mustSaveSQL(t, fixture.database.SQL, `
INSERT INTO rpgmaker_runtime_validations(
 id,import_item_id,review_version_at_create,runtime_binding_revision,effective_source_snapshot_id,
 project_fingerprint,core_id,generation,evidence_generation,evidence_confidence,route_key,artifact_id,
 artifact_set_sha256,adapter_id,adapter_abi,dependency_snapshot_sha256,state,machine_gates_json,
 created_at_ms,updated_at_ms,expires_at_ms)
VALUES(?,?,1,1,?,?,'rpgmaker_2000','RPG2000',NULL,'FAMILY_ONLY','RPG2000_EASYRPG_SAVE_TEST',
 ?,?,'easyrpg-web-v1','easy-save-abi',?,'CREATED','{}',?,?,?)`, validationID, ids["item"],
		ids["snapshot"], strings.Repeat("5", 64), artifactID, strings.Repeat("c", 64),
		strings.Repeat("7", 64), now, now, now+900_000)
	mustSaveSQL(t, fixture.database.SQL, `
INSERT INTO launch_sessions(
 id,profile_id,purpose,core_artifact_id,route_key,effective_source_snapshot_id,
 rpgmaker_runtime_validation_id,return_to,credential_sha256,state,bootstrap_expires_at_ms,
 hard_expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'local','RPG_RUNTIME_VALIDATION',?,'RPG2000_EASYRPG_SAVE_TEST',?,?,?,?,'CREATED',?,?,?,?)`,
		launchID, artifactID, ids["snapshot"], validationID, "/admin/reviews/"+ids["item"], capabilityHash[:],
		now+60_000, now+900_000, now, now)
	mustSaveSQL(t, fixture.database.SQL, `
UPDATE rpgmaker_runtime_validations SET launch_id=?,state='STARTING',updated_at_ms=? WHERE id=?`,
		launchID, now, validationID)
	mustSaveSQL(t, fixture.database.SQL, `
UPDATE rpgmaker_runtime_validations SET state='RUNNING',updated_at_ms=? WHERE id=?`, now, validationID)
	mustSaveSQL(t, fixture.database.SQL, `
UPDATE launch_sessions SET state='ACTIVE',activated_at_ms=?,idle_expires_at_ms=?,updated_at_ms=?,version=version+1
WHERE id=?`, now, now+120_000, now, launchID)
	return saveLaunch{
		LaunchID: launchID, Capability: retromruntime.EncodeCapability(capability),
	}, validationID
}

func (fixture *saveFixture) createValidationRestoreLaunch(t *testing.T, validationID string) saveLaunch {
	t.Helper()
	launchID := uuid.NewString()
	launchUUID := uuid.MustParse(launchID)
	capability := fixture.credentials.Capability(launchUUID)
	capabilityHash := retromruntime.HashCapability(capability)
	now := fixture.now.UnixMilli()
	mustSaveSQL(t, fixture.database.SQL, `
INSERT INTO launch_sessions(
 id,profile_id,purpose,core_artifact_id,route_key,effective_source_snapshot_id,
 rpgmaker_runtime_validation_id,return_to,credential_sha256,state,bootstrap_expires_at_ms,
 hard_expires_at_ms,created_at_ms,updated_at_ms)
SELECT ?,'local','RPG_RUNTIME_VALIDATION',artifact_id,route_key,effective_source_snapshot_id,id,
 '/admin/reviews/'||import_item_id,?,'CREATED',?,?,?,?
FROM rpgmaker_runtime_validations WHERE id=?`, launchID, capabilityHash[:], now+60_000,
		now+900_000, now, now, validationID)
	mustSaveSQL(t, fixture.database.SQL, `
UPDATE rpgmaker_runtime_validations SET restore_launch_id=?,updated_at_ms=? WHERE id=?`,
		launchID, now, validationID)
	mustSaveSQL(t, fixture.database.SQL, `
UPDATE launch_sessions SET state='ACTIVE',activated_at_ms=?,idle_expires_at_ms=?,updated_at_ms=?,version=version+1
WHERE id=?`, now, now+120_000, now, launchID)
	return saveLaunch{
		LaunchID: launchID, Capability: retromruntime.EncodeCapability(capability),
	}
}

func mustSaveSQL(t *testing.T, database *sql.DB, query string, arguments ...any) {
	t.Helper()
	if _, err := database.Exec(query, arguments...); err != nil {
		t.Fatalf("save fixture SQL: %v\n%s", err, query)
	}
}
