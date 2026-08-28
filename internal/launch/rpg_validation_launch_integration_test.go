//go:build integration

package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/libraryimport"
	"retrom/internal/rpgmaker/detector"
	"retrom/internal/rpgmaker/routing"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/testsupport"
)

func TestRPGValidationLaunchLocksProjectAndRestoresAfterTerminalOriginal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	now := time.UnixMilli(1_786_000_000_000)
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	seedLocalProfile(t, database.SQL)
	fixture := seedRPGValidationLaunchFixture(t, database.SQL, now.UnixMilli())
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	service := New(database.SQL, nil, credentials, func() time.Time { return now })
	created, err := service.CreateRPGValidation(
		ctx, "local", fixture.validationID, "/admin/reviews/"+fixture.itemID, Capabilities{},
	)
	if err != nil || created.LaunchID == "" || created.Existing {
		t.Fatalf("create RPG validation launch = %#v, error=%v", created, err)
	}
	replayed, err := service.CreateRPGValidation(
		ctx, "local", fixture.validationID, "/admin/reviews/"+fixture.itemID, Capabilities{},
	)
	if err != nil || !replayed.Existing || replayed.LaunchID != created.LaunchID ||
		replayed.Capability != created.Capability {
		t.Fatalf("replay RPG validation launch = %#v, error=%v", replayed, err)
	}
	configuration, err := service.Config(ctx, created.LaunchID, created.Capability)
	if err != nil || configuration.RPGMaker == nil || configuration.RPGMaker.Purpose != "RPG_RUNTIME_VALIDATION" ||
		configuration.RPGMaker.RuntimeValidation == nil ||
		configuration.RPGMaker.RuntimeValidation.OriginalLaunchID != created.LaunchID ||
		configuration.RPGMaker.RuntimeValidation.LastGateSequence != 0 ||
		len(configuration.RPGMaker.RuntimeValidation.MachineGates) != 14 {
		t.Fatalf("RPG validation config = %#v, error=%v", configuration, err)
	}
	assertRPGValidationState(t, database.SQL, fixture.validationID, "STARTING")
	encoded, err := MarshalConfig(configuration)
	var union map[string]any
	if err != nil || json.Unmarshal(encoded, &union) != nil || union["runtimeFamily"] != "RPGMAKER" ||
		union["adapter"].(map[string]any)["adapterKind"] != "EASYRPG_WEB" {
		t.Fatalf("RPG config union = %s, error=%v", encoded, err)
	}
	assertRPGValidationConfigJSONShape(t, union, 0, created.LaunchID, nil)
	assertRPGValidationContent(t, ctx, database.SQL, service, created, fixture)
	start := PlayEvent{ClientSequence: 0, ClientObservedAtMS: now.UnixMilli()}
	started, err := service.RecordPlay(ctx, created.LaunchID, created.Capability, "start", start)
	if err != nil || started.PlaySessionID != nil || started.State != "ACTIVE" {
		t.Fatalf("start RPG validation play = %#v, error=%v", started, err)
	}
	assertRPGPlaySessionCount(t, database.SQL, created.LaunchID, 0)
	checkpointRPGValidation(t, database.SQL, fixture, created.LaunchID, now.UnixMilli())
	if _, err := service.CreateRPGValidationRestore(
		ctx, "local", fixture.validationID, "/admin/reviews/"+fixture.itemID, Capabilities{},
	); !errors.Is(err, ErrBlocked) {
		t.Fatalf("restore before original terminal error = %v", err)
	}
	finished, err := service.RecordPlay(ctx, created.LaunchID, created.Capability, "finish", PlayEvent{
		ClientSequence: 1, ClientObservedAtMS: now.UnixMilli() + 1,
		PreviousInterval: &Interval{Running: true, Visible: true},
	})
	if err != nil || finished.PlaySessionID != nil || finished.State != "FINISHED" {
		t.Fatalf("finish RPG validation play = %#v, error=%v", finished, err)
	}
	assertRPGPlaySessionCount(t, database.SQL, created.LaunchID, 0)
	restore, err := service.CreateRPGValidationRestore(
		ctx, "local", fixture.validationID, "/admin/reviews/"+fixture.itemID, Capabilities{},
	)
	if err != nil || restore.LaunchID == "" || restore.LaunchID == created.LaunchID {
		t.Fatalf("create RPG validation restore = %#v, error=%v", restore, err)
	}
	assertCopiedRPGValidationContent(t, database.SQL, created.LaunchID, restore.LaunchID)
	restoreConfig, err := service.Config(ctx, restore.LaunchID, restore.Capability)
	if err != nil || restoreConfig.RPGMaker == nil || restoreConfig.RPGMaker.Checkpoint == nil ||
		restoreConfig.RPGMaker.Checkpoint.PayloadURL != "/runtime/launches/"+restore.LaunchID+"/state" ||
		restoreConfig.RPGMaker.RuntimeValidation == nil ||
		restoreConfig.RPGMaker.RuntimeValidation.OriginalLaunchID != created.LaunchID ||
		restoreConfig.RPGMaker.RuntimeValidation.RestoreLaunchID == nil ||
		*restoreConfig.RPGMaker.RuntimeValidation.RestoreLaunchID != restore.LaunchID ||
		restoreConfig.RPGMaker.RuntimeValidation.LastGateSequence != 20 ||
		restoreConfig.RPGMaker.RuntimeValidation.CheckpointEvidence == nil {
		t.Fatalf("RPG restore config = %#v, error=%v", restoreConfig, err)
	}
	restoreEncoded, err := MarshalConfig(restoreConfig)
	var restoreUnion map[string]any
	if err != nil || json.Unmarshal(restoreEncoded, &restoreUnion) != nil {
		t.Fatalf("marshal RPG restore config = %s, error=%v", restoreEncoded, err)
	}
	assertRPGValidationConfigJSONShape(t, restoreUnion, 20, created.LaunchID, &restore.LaunchID)
	assertRPGPlaySessionCount(t, database.SQL, restore.LaunchID, 0)
}

func TestRPGValidationLaunchAllowsReviewApprovalBeforeOptionalMachineGates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	now := time.UnixMilli(1_786_000_000_000)
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	seedLocalProfile(t, database.SQL)
	fixture := seedRPGValidationLaunchFixture(t, database.SQL, now.UnixMilli())
	mustRPGLaunchSQL(t, database.SQL, `UPDATE review_drafts SET metadata_json='{"title":"RPG smoke"}' WHERE import_item_id=?`, fixture.itemID)
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	launcher := New(database.SQL, nil, credentials, func() time.Time { return now })
	created, err := launcher.CreateRPGValidation(
		ctx, "local", fixture.validationID, "/admin/reviews/"+fixture.itemID, Capabilities{},
	)
	if err != nil {
		t.Fatalf("create RPG validation launch: %v", err)
	}
	approved, err := libraryimport.New(database.SQL, func() time.Time { return now }).Approve(ctx, fixture.itemID, 2)
	if err != nil {
		t.Fatalf("approve after RPG validation launch: %v", err)
	}
	var validationID, validationState string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT profile.runtime_validation_id,validation.state
FROM games game
JOIN game_variants variant ON variant.game_id=game.id
JOIN rpgmaker_variant_profiles profile ON profile.game_variant_revision_id=variant.current_revision_id
JOIN rpgmaker_runtime_validations validation ON validation.id=profile.runtime_validation_id
WHERE game.id=?`, approved.GameID).Scan(&validationID, &validationState); err != nil {
		t.Fatal(err)
	}
	if validationID != fixture.validationID || validationState != "STARTING" || created.LaunchID == "" {
		t.Fatalf("published RPG launch evidence = %s/%s/%s", validationID, validationState, created.LaunchID)
	}
}

func assertRPGValidationConfigJSONShape(
	t *testing.T,
	configuration map[string]any,
	lastSequence float64,
	originalLaunchID string,
	restoreLaunchID *string,
) {
	t.Helper()
	assertRPGJSONKeys(t, configuration,
		"adapter", "artifactId", "checkpoint", "checkpointAvailability", "coreId", "coreName",
		"gameTitle", "generation", "launchId", "mode", "platformName", "protocolVersion", "purpose",
		"returnTo", "routeKey", "runtimeFamily", "runtimeValidation", "warnings",
	)
	resume, ok := configuration["runtimeValidation"].(map[string]any)
	if !ok {
		t.Fatalf("runtimeValidation JSON = %#v", configuration["runtimeValidation"])
	}
	assertRPGJSONKeys(t, resume,
		"checkpointEvidence", "lastGateSequence", "machineGates", "originalLaunchId", "restoreLaunchId",
		"restoreScreenshotUploaded", "state", "validationId",
	)
	if resume["lastGateSequence"] != lastSequence || resume["originalLaunchId"] != originalLaunchID {
		t.Fatalf("runtimeValidation resume = %#v", resume)
	}
	if restoreLaunchID == nil && resume["restoreLaunchId"] != nil ||
		restoreLaunchID != nil && resume["restoreLaunchId"] != *restoreLaunchID {
		t.Fatalf("runtimeValidation restoreLaunchId = %#v, want %#v", resume["restoreLaunchId"], restoreLaunchID)
	}
	gates, ok := resume["machineGates"].([]any)
	if !ok || len(gates) != 14 {
		t.Fatalf("runtimeValidation machineGates = %#v", resume["machineGates"])
	}
	for _, value := range gates {
		gate, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("runtimeValidation gate = %#v", value)
		}
		assertRPGJSONKeys(t, gate,
			"begunAtMs", "completedAtMs", "evidence", "failureCode", "gate", "status",
		)
	}
}

func assertRPGJSONKeys(t *testing.T, value map[string]any, want ...string) {
	t.Helper()
	if len(value) != len(want) {
		t.Fatalf("JSON keys = %#v, want %#v", value, want)
	}
	for _, key := range want {
		if _, ok := value[key]; !ok {
			t.Fatalf("JSON missing key %q in %#v", key, value)
		}
	}
}

func TestRPGProjectContentUsesOnlyUniqueASCIICaseFoldFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.UnixMilli(1_786_000_000_000)
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(t.TempDir(), "retrom.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	seedLocalProfile(t, database.SQL)
	fixture := seedRPGValidationLaunchFixture(t, database.SQL, now.UnixMilli())
	credentials, err := retromruntime.LoadOrCreateCredentials(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := New(database.SQL, nil, credentials, func() time.Time { return now })
	created, err := service.CreateRPGValidation(
		ctx, "local", fixture.validationID, "/admin/reviews/"+fixture.itemID, Capabilities{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Config(ctx, created.LaunchID, created.Capability); err != nil {
		t.Fatal(err)
	}

	exact, err := service.RPGProjectContentAuthorized(ctx, created.LaunchID, "RPG_RT.ldb")
	if err != nil || exact.Digest != fixture.projectSHA {
		t.Fatalf("exact RPG content = %#v, error=%v", exact, err)
	}
	folded, err := service.RPGProjectContentAuthorized(ctx, created.LaunchID, "rpg_rt.LDB")
	if err != nil || folded.Digest != fixture.projectSHA {
		t.Fatalf("folded RPG content = %#v, error=%v", folded, err)
	}
	if _, err := service.ContentAuthorized(ctx, created.LaunchID, "rpg_rt.LDB"); !errors.Is(err, ErrCredential) {
		t.Fatalf("ordinary content accepted folded path: %v", err)
	}

	mustRPGLaunchSQL(t, database.SQL, `
	INSERT INTO launch_content_files(launch_session_id,logical_name,blob_id,format_version,created_at_ms)
	SELECT launch_session_id,'rpg_rt.ldb',blob_id,format_version,created_at_ms
FROM launch_content_files
WHERE launch_session_id=? AND logical_name='RPG_RT.ldb'`, created.LaunchID)
	if _, err := service.RPGProjectContentAuthorized(ctx, created.LaunchID, "RpG_Rt.LdB"); !errors.Is(err, ErrCredential) {
		t.Fatalf("ambiguous folded RPG content accepted: %v", err)
	}
}

type rpgValidationLaunchFixture struct {
	validationID, itemID, projectBlobID, projectSHA, indexBlobID string
}

func seedRPGValidationLaunchFixture(
	t *testing.T,
	database *sql.DB,
	now int64,
) rpgValidationLaunchFixture {
	t.Helper()
	fixture := rpgValidationLaunchFixture{
		validationID: "rpg-validation", itemID: "rpg-item", projectBlobID: "rpg-project-a",
		projectSHA: strings.Repeat("1", 64), indexBlobID: "rpg-index",
	}
	for _, blob := range []struct{ id, sha string }{
		{fixture.projectBlobID, fixture.projectSHA},
		{"rpg-project-b", strings.Repeat("2", 64)},
		{fixture.indexBlobID, strings.Repeat("3", 64)},
		{"rpg-checkpoint", strings.Repeat("4", 64)},
	} {
		mustRPGLaunchSQL(t, database, `
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES(?,?,10,?,?,?,'application/octet-stream',?)`, blob.id, blob.sha, strings.Repeat("a", 32),
			strings.Repeat("b", 40), strings.Repeat("c", 8), now)
	}
	artifactSet := strings.Repeat("5", 64)
	currentRoute, err := routing.Current("rpgmaker_2000", detector.RPG2000)
	if err != nil {
		t.Fatal(err)
	}
	mustRPGLaunchSQL(t, database, `
INSERT INTO core_artifacts(
 id,core_id,route_key,runtime_family,runtime_adapter_kind,runtime_version,adapter_id,entry_path,
 size_bytes,sha256,manifest_sha256,artifact_set_sha256,requires_threads,save_payload_kind,
 save_max_bytes,provenance_json,compatibility_json,selected_for_new_bindings,available_for_launch,
 version,created_at_ms,updated_at_ms)
VALUES('rpg-artifact','rpgmaker_2000','RPG2000_EASYRPG','RPGMAKER','EASYRPG_WEB',
 ?,'easyrpg-web','runtime/easyrpg.js',1,?,?,?,0,'NATIVE_SAVE_BUNDLE_V1',
 67108864,'{}','{}',1,1,1,?,?)`, currentRoute.RuntimeVersion,
		strings.Repeat("6", 64), strings.Repeat("7", 64), artifactSet, now, now)
	mustRPGLaunchSQL(t, database, `
INSERT INTO platform_instances(
 id,platform_id,default_core_id,name,slug,sort_order,enabled,version,created_at_ms,updated_at_ms)
VALUES('rpg-platform','rpgmaker','rpgmaker','RPG Maker validation','rpg-validation',999,1,1,?,?)`, now, now)
	mustRPGLaunchSQL(t, database, `
INSERT INTO upload_sessions(id,purpose,state,source_type,total_files,total_bytes,manifest_digest,
 expires_at_ms,created_at_ms,updated_at_ms)
VALUES('rpg-upload','RPG_MAKER_PROJECT','COMPLETE','DIRECTORY',2,20,?,?,?,?)`,
		strings.Repeat("8", 64), now+1_000_000, now, now)
	for index, file := range []struct{ id, path, blob string }{
		{"rpg-upload-a", "RPG_RT.ldb", fixture.projectBlobID},
		{"rpg-upload-b", "Map0001.lmu", "rpg-project-b"},
	} {
		mustRPGLaunchSQL(t, database, `
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,
 final_blob_id,state,created_at_ms,updated_at_ms)
VALUES(?,'rpg-upload',?,10,10,?,'COMPLETE',?,?)`, file.id, file.path, file.blob, now+int64(index), now+int64(index))
	}
	mustRPGLaunchSQL(t, database, `
INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,
 platform_id,default_core_id,core_artifact_id,metadata_provider,config_snapshot_json,
 config_snapshot_digest,state,total_item_count,review_pending_item_count,created_at_ms,updated_at_ms)
VALUES('rpg-import','rpg-upload','rpg-platform',1,'rpgmaker','rpgmaker_2000','rpg-artifact',
 'NONE','{}',?,'REVIEW_PENDING',1,1,?,?)`, strings.Repeat("9", 64), now, now)
	mustRPGLaunchSQL(t, database, `
INSERT INTO import_items(id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,
 search_text,created_at_ms,updated_at_ms)
VALUES(?,'rpg-import',?,'REVIEW_PENDING','{}',?,'rpg fixture',?,?)`, fixture.itemID,
		strings.Repeat("a", 64), strings.Repeat("b", 64), now, now)
	manifest := `{"schemaVersion":2,"contentKind":"RPG_MAKER_PROJECT_V1","fileCount":2,"totalBytes":20,"filesDigest":"` +
		strings.Repeat("c", 64) + `"}`
	mustRPGLaunchSQL(t, database, `
INSERT INTO import_item_source_snapshots(id,import_item_id,revision_no,content_kind,
 source_manifest_json,source_manifest_digest,created_by,created_at_ms)
VALUES('rpg-snapshot',?,1,'RPG_MAKER_PROJECT_V1',?,?,'IDENTIFICATION',?)`, fixture.itemID,
		manifest, strings.Repeat("d", 64), now)
	for index, file := range []struct{ upload, logical, blob string }{
		{"rpg-upload-a", "RPG_RT.ldb", fixture.projectBlobID},
		{"rpg-upload-b", "Map0001.lmu", "rpg-project-b"},
	} {
		mustRPGLaunchSQL(t, database, `
INSERT INTO import_item_source_snapshot_files(source_snapshot_id,role,logical_name,upload_file_id,
 blob_id,sort_order,created_at_ms)
VALUES('rpg-snapshot','PROJECT_FILE',?,?,?, ?,?)`, file.logical, file.upload, file.blob, index, now)
	}
	mustRPGLaunchSQL(t, database, `
INSERT INTO review_drafts(id,import_item_id,target_platform_instance_id,metadata_json,
 runtime_binding_revision,version,created_at_ms,updated_at_ms,effective_source_snapshot_id)
VALUES('01980000-0000-7000-8000-000000000901',?,'rpg-platform','{}',1,1,?,?,'rpg-snapshot')`, fixture.itemID, now, now)
	mustRPGLaunchSQL(t, database, `
INSERT INTO import_item_core_validations(id,import_item_id,target_platform_instance_id,
 platform_instance_version,core_id,core_artifact_id,core_artifact_version,prepublish_generation,
 source_manifest_digest,source_snapshot_id,prepublish_input_digest,status,compatibility_code,
 dependency_snapshot_json,created_at_ms)
VALUES('rpg-core-validation',?,'rpg-platform',1,'rpgmaker_2000','rpg-artifact',1,4,?,
 'rpg-snapshot',?,'BLOCKED','RPG_RUNTIME_VALIDATION_REQUIRED','{}',?)`, fixture.itemID, strings.Repeat("d", 64),
		strings.Repeat("e", 64), now)
	mustRPGLaunchSQL(t, database, `
INSERT INTO import_item_validation_files(import_item_core_validation_id,role,logical_name,blob_id,
 sort_order,created_at_ms)
VALUES('rpg-core-validation','RPG_EASYRPG_INDEX','index.json',?,0,?)`, fixture.indexBlobID, now)
	mustRPGLaunchSQL(t, database, `
UPDATE review_drafts SET version=version+1,updated_at_ms=?
WHERE id='01980000-0000-7000-8000-000000000901'`, now)
	projectFingerprint, dependency := strings.Repeat("c", 64), strings.Repeat("f", 64)
	mustRPGLaunchSQL(t, database, `
INSERT INTO rpgmaker_review_profiles(
 review_draft_id,selected_core_id,generation,evidence_family,evidence_generation,evidence_confidence,
 file_count,total_bytes,project_fingerprint,requirements_sha256,analysis_json,self_contained_override,
 route_key,artifact_id,artifact_set_sha256,adapter_id,adapter_abi,dependency_snapshot_sha256,
 created_at_ms,updated_at_ms)
VALUES('01980000-0000-7000-8000-000000000901','rpgmaker_2000','RPG2000','RPG2K','RPG2000','MATCHED',2,20,?,?,'{}',1,
 'RPG2000_EASYRPG','rpg-artifact',?,'easyrpg-web','easyrpg-save',?,?,?)`,
		projectFingerprint, strings.Repeat("0", 64), artifactSet, dependency, now, now)
	mustRPGLaunchSQL(t, database, `
INSERT INTO rpgmaker_runtime_validations(
 id,import_item_id,review_version_at_create,runtime_binding_revision,effective_source_snapshot_id,
 project_fingerprint,core_id,generation,evidence_generation,evidence_confidence,route_key,artifact_id,
 artifact_set_sha256,adapter_id,adapter_abi,dependency_snapshot_sha256,state,machine_gates_json,
 created_at_ms,updated_at_ms,expires_at_ms)
VALUES(?,?,2,1,'rpg-snapshot',?,'rpgmaker_2000','RPG2000','RPG2000','MATCHED',
 'RPG2000_EASYRPG','rpg-artifact',?,'easyrpg-web','easyrpg-save',?,
 'CREATED',?,?,?,?)`, fixture.validationID, fixture.itemID, projectFingerprint, artifactSet,
		dependency, rpgValidationMachineGatesJSON(t, -1), now, now, now+900_000)
	return fixture
}

func assertRPGValidationContent(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	service *Service,
	created Created,
	fixture rpgValidationLaunchFixture,
) {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT count(*) FROM launch_content_files WHERE launch_session_id=?`,
		created.LaunchID).Scan(&count); err != nil || count != 3 {
		t.Fatalf("locked RPG content count = %d, error=%v", count, err)
	}
	content, err := service.Content(ctx, created.LaunchID, created.Capability, "RPG_RT.ldb")
	if err != nil || content.Digest != fixture.projectSHA || content.Format != rpgProjectFormat {
		t.Fatalf("locked RPG project content = %#v, error=%v", content, err)
	}
	index, err := service.Content(ctx, created.LaunchID, created.Capability, rpgEasyIndexName)
	if err != nil || index.Digest == "" || index.Format != rpgProjectFormat {
		t.Fatalf("locked EasyRPG index = %#v, error=%v", index, err)
	}
}

func checkpointRPGValidation(
	t *testing.T,
	database *sql.DB,
	fixture rpgValidationLaunchFixture,
	launchID string,
	now int64,
) {
	t.Helper()
	mustRPGLaunchSQL(t, database, `
UPDATE rpgmaker_runtime_validations SET state='RUNNING',updated_at_ms=? WHERE id=?`, now, fixture.validationID)
	mustRPGLaunchSQL(t, database, `
INSERT INTO rpgmaker_runtime_validation_checkpoints(
 validation_id,payload_blob_id,payload_kind,native_profile,resume_slot,payload_sha256,size_bytes,created_at_ms)
VALUES(?,'rpg-checkpoint','NATIVE_SAVE_BUNDLE_V1','EASYRPG_V1',100,?,10,?)`,
		fixture.validationID, strings.Repeat("4", 64), now)
	appendRPGValidationOriginalGates(t, database, fixture.validationID, launchID, now)
	mustRPGLaunchSQL(t, database, `
UPDATE rpgmaker_runtime_validations SET state='CHECKPOINTED',machine_gates_json=?,updated_at_ms=? WHERE id=?`,
		rpgValidationMachineGatesJSON(t, 9), now, fixture.validationID)
}

func appendRPGValidationOriginalGates(
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
	for index, gate := range gates {
		for _, phase := range []string{"BEGIN", "PASS"} {
			sequence := int64(index*2 + map[bool]int{true: 1, false: 2}[phase == "BEGIN"])
			evidence := rpgValidationGateEvidence(gate, phase)
			mustRPGLaunchSQL(t, database, `
INSERT INTO rpgmaker_runtime_validation_gate_events(
 validation_id,sequence,event_id,launch_id,gate,phase,observed_at_ms,evidence_json,created_at_ms)
VALUES(?,?,?,?,?,?,?,?,?)`, validationID, sequence, uuid.NewString(), launchID, gate, phase,
				now+sequence, evidence, now+sequence)
			mustRPGLaunchSQL(t, database, `
UPDATE rpgmaker_runtime_validations SET last_gate_sequence=?,updated_at_ms=? WHERE id=?`,
				sequence, now+sequence, validationID)
		}
	}
}

func rpgValidationGateEvidence(gate, phase string) string {
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

func rpgValidationMachineGatesJSON(t *testing.T, passedThrough int) string {
	t.Helper()
	gates := []string{
		"RUNTIME_READY", "ENGINE_PROFILE", "FRAMES_300", "INPUT", "AUDIO",
		"INITIAL_POSITION_RECORDED", "SAVE_POINT_RECORDED", "CHECKPOINT_CREATED",
		"POST_SAVE_STATE_DIVERGED", "ORIGINAL_LAUNCH_ENDED", "RESTORE_STARTED",
		"RESTORE_POSITION_VERIFIED", "RESTORE_SCREENSHOT", "RESTORE_INPUT",
	}
	projection := make([]map[string]any, 0, len(gates))
	for index, gate := range gates {
		status := "NOT_STARTED"
		var begun, completed any
		var evidence any
		if index <= passedThrough {
			status, begun, completed = "PASSED", int64(index*2+1), int64(index*2+2)
			evidence = json.RawMessage(rpgValidationGateEvidence(gate, "PASS"))
		}
		projection = append(projection, map[string]any{
			"gate": gate, "status": status, "begunAtMs": begun, "completedAtMs": completed,
			"evidence": evidence, "failureCode": nil,
		})
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func assertRPGValidationState(t *testing.T, database *sql.DB, validationID, expected string) {
	t.Helper()
	var state string
	if err := database.QueryRow(
		`SELECT state FROM rpgmaker_runtime_validations WHERE id=?`, validationID,
	).Scan(&state); err != nil || state != expected {
		t.Fatalf("RPG validation state = %q, want %q, error=%v", state, expected, err)
	}
}

func assertCopiedRPGValidationContent(t *testing.T, database *sql.DB, originalID, restoreID string) {
	t.Helper()
	var same int
	if err := database.QueryRow(`
SELECT NOT EXISTS(
 SELECT logical_name,blob_id,format_version FROM launch_content_files WHERE launch_session_id=?
 EXCEPT SELECT logical_name,blob_id,format_version FROM launch_content_files WHERE launch_session_id=?
) AND NOT EXISTS(
 SELECT logical_name,blob_id,format_version FROM launch_content_files WHERE launch_session_id=?
 EXCEPT SELECT logical_name,blob_id,format_version FROM launch_content_files WHERE launch_session_id=?
)`, originalID, restoreID, restoreID, originalID).Scan(&same); err != nil || same != 1 {
		t.Fatalf("restore content differs from original: same=%d error=%v", same, err)
	}
}

func assertRPGPlaySessionCount(t *testing.T, database *sql.DB, launchID string, expected int) {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT count(*) FROM play_sessions WHERE launch_session_id=?`, launchID).
		Scan(&count); err != nil || count != expected {
		t.Fatalf("RPG validation play session count = %d, want %d, error=%v", count, expected, err)
	}
}

func mustRPGLaunchSQL(t *testing.T, database *sql.DB, query string, arguments ...any) {
	t.Helper()
	if _, err := database.Exec(query, arguments...); err != nil {
		t.Fatalf("RPG launch fixture SQL: %v\n%s", err, query)
	}
}
