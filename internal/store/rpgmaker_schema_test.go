package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/testassert"
)

const (
	rpgSchemaProvider       = "retrom-runtime"
	rpgSchemaTarget         = "rpgmaker-2000"
	rpgSchemaTargetContract = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	rpgSchemaGameLine       = "rpgmaker-2000-v1"
	rpgSchemaBundle         = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	rpgSchemaValidation     = "rpg-validation"
	rpgSchemaOriginal       = "rpg-original-launch"
	rpgSchemaRestore        = "rpg-restore-launch"
)

func TestRPGMakerProviderTargetSelectionAndCoreRouteAreDatabaseConstraints(t *testing.T) {
	t.Parallel()
	database := openRPGMakerSchemaDatabase(t)
	defer func() { cleanup.Error("close", database.Close()) }()
	insertRPGMakerProviderProjection(t, database)
	mustExecRPGSchema(t, database, `
INSERT INTO runtime_target_bindings(
 binding_id,core_id,provider_id,target_id,detector_profile,delivery_profile,launch_policy,review_policy
) VALUES('retrom-runtime-rpgmaker-2000','rpgmaker',?1,?2,'RPG2000','FILE_TREE_PROJECT_V1','SUPPORTED','RPG_RUNTIME_VALIDATION_V1')`,
		rpgSchemaProvider, rpgSchemaTarget)
	_, err := database.ExecContext(t.Context(), `
INSERT INTO runtime_target_bindings(
 binding_id,core_id,provider_id,target_id,detector_profile,delivery_profile,launch_policy,review_policy
) VALUES('second-selected','fceumm',?1,?2,'RPG2000','FILE_TREE_PROJECT_V1','SUPPORTED','RPG_RUNTIME_VALIDATION_V1')`,
		rpgSchemaProvider, rpgSchemaTarget)
	testassert.Truef(t, err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed"),
		"second binding for Provider Target error = %v", err)
	_, err = database.ExecContext(t.Context(), `
INSERT INTO runtime_binding_platforms(binding_id,platform_id,core_id)
VALUES('retrom-runtime-rpgmaker-2000','nes','rpgmaker')`)
	testassert.Truef(t, err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed"),
		"cross-core platform route error = %v", err)
}

func TestRuntimePackFilesAreStagedContiguouslyBeforeReady(t *testing.T) {
	t.Parallel()
	database := openRPGMakerSchemaDatabase(t)
	defer func() { cleanup.Error("close", database.Close()) }()
	insertSchemaIdentity(t, database)
	insertSchemaBlob(t, database, "pack-blob", "b", 10)
	mustExecRPGSchema(t, database, `
INSERT INTO runtime_asset_pack_installations(
 id,definition_id,files_digest,file_count,total_bytes,bundle_blob_id,bundle_sha256,status,
 diagnostic_json,created_by_user_id,created_at_ms
) VALUES('pack','rpg2000_rtp',?1,1,10,'pack-blob',?2,'VALIDATING','{}','user',1)
`, strings.Repeat("c", 64), strings.Repeat("b", 64))
	_, err := database.ExecContext(context.Background(), `
INSERT INTO runtime_asset_pack_files(installation_id,path,ordinal,blob_id,size_bytes,sha256)
VALUES('pack','Music/a.wav',1,'pack-blob',10,?)`, strings.Repeat("b", 64))
	testassert.Truef(t, err != nil && strings.Contains(err.Error(), "invalid runtime pack file"),
		"non-contiguous staged pack ordinal error = %v", err)
	mustExecRPGSchema(t, database, `
INSERT INTO runtime_asset_pack_files(installation_id,path,ordinal,blob_id,size_bytes,sha256)
VALUES('pack','Music/a.wav',0,'pack-blob',10,?1)`, strings.Repeat("b", 64))
	mustExecRPGSchema(t, database, `
	UPDATE runtime_asset_pack_installations SET status='READY',validated_at_ms=2,version=version+1 WHERE id='pack'`)
	_, err = database.ExecContext(context.Background(), `
INSERT INTO runtime_asset_pack_files(installation_id,path,ordinal,blob_id,size_bytes,sha256)
VALUES('pack','Music/b.wav',1,'pack-blob',10,?)`, strings.Repeat("b", 64))
	testassert.Truef(t, err != nil && strings.Contains(err.Error(), "invalid runtime pack file"),
		"READY pack accepted new file: %v", err)
}

func TestRuntimeValidationEnforcesInitialSaveRestoreAndPostRestoreInputPositions(t *testing.T) {
	t.Parallel()
	database := openRPGMakerSchemaDatabase(t)
	defer func() { cleanup.Error("close", database.Close()) }()
	preparePositionValidation(t, database)
	sequence := 13
	appendGateBegin(t, database, &sequence, rpgSchemaOriginal, "SAVE_POINT_RECORDED")
	assertPositionPassRejected(t, database, sequence, rpgSchemaOriginal, "SAVE_POINT_RECORDED", 10, 0)
	appendPositionPass(t, database, &sequence, rpgSchemaOriginal, "SAVE_POINT_RECORDED", 7, 11, 13, 1)
	insertValidationCheckpoint(t, database)
	appendEmptyGatePair(t, database, &sequence, rpgSchemaOriginal, "CHECKPOINT_CREATED")
	mustExecRPGSchema(t, database, `UPDATE rpgmaker_runtime_validations SET state='CHECKPOINTED' WHERE id=?1`, rpgSchemaValidation)
	appendGateBegin(t, database, &sequence, rpgSchemaOriginal, "POST_SAVE_STATE_DIVERGED")
	assertPositionPassRejected(t, database, sequence, rpgSchemaOriginal, "POST_SAVE_STATE_DIVERGED", 11, 1)
	appendPositionPass(t, database, &sequence, rpgSchemaOriginal, "POST_SAVE_STATE_DIVERGED", 7, 12, 13, 2)
	appendEmptyGatePair(t, database, &sequence, rpgSchemaOriginal, "ORIGINAL_LAUNCH_ENDED")
	attachSchemaRestoreLaunch(t, database)
	appendEmptyGatePair(t, database, &sequence, rpgSchemaRestore, "RESTORE_STARTED")
	appendGateBegin(t, database, &sequence, rpgSchemaRestore, "RESTORE_POSITION_VERIFIED")
	assertPositionPassRejected(t, database, sequence, rpgSchemaRestore, "RESTORE_POSITION_VERIFIED", 12, 2)
	appendPositionPass(t, database, &sequence, rpgSchemaRestore, "RESTORE_POSITION_VERIFIED", 7, 11, 13, 1)
	mustExecRPGSchema(t, database, `UPDATE rpgmaker_runtime_validations SET state='RESTORED' WHERE id=?1`, rpgSchemaValidation)
	mustExecRPGSchema(t, database, `UPDATE rpgmaker_runtime_validations SET evidence_screenshot_blob_id='checkpoint-blob' WHERE id=?1`, rpgSchemaValidation)
	appendEmptyGatePair(t, database, &sequence, rpgSchemaRestore, "RESTORE_SCREENSHOT")
	appendGateBegin(t, database, &sequence, rpgSchemaRestore, "RESTORE_INPUT")
	assertPositionPassRejected(t, database, sequence, rpgSchemaRestore, "RESTORE_INPUT", 11, 1)
	appendPositionPass(t, database, &sequence, rpgSchemaRestore, "RESTORE_INPUT", 7, 13, 13, 3)
	mustExecRPGSchema(t, database, `UPDATE rpgmaker_runtime_validations SET state='AWAITING_DECISION' WHERE id=?1`, rpgSchemaValidation)
}

func TestLaunchTerminationAcceptsCapabilityAlreadyRevokedByRuntimeCleanup(t *testing.T) {
	t.Parallel()
	database := openRPGMakerSchemaDatabase(t)
	defer func() { cleanup.Error("close", database.Close()) }()
	preparePositionValidation(t, database)
	// This test isolates the interaction between the capability's one-way revoke
	// trigger and the Launch terminal-state cascade. Ticket consumption itself is
	// covered by the isolation service tests.
	mustExecRPGSchema(t, database, `DROP TRIGGER isolated_runtime_capabilities_insert`)
	mustExecRPGSchema(t, database, `
INSERT INTO isolated_runtime_capabilities(
 credential_sha256,launch_id,profile_id,expected_origin,issued_at_ms,expires_at_ms
) VALUES(?1,?2,'profile','https://rpg-original-launch.rpg-runtime.example',1,100)`,
		[]byte(strings.Repeat("c", 32)), rpgSchemaOriginal)
	mustExecRPGSchema(t, database, `
UPDATE isolated_runtime_capabilities SET revoked_at_ms=2 WHERE launch_id=?1`, rpgSchemaOriginal)
	mustExecRPGSchema(t, database, `
UPDATE launch_sessions SET state='REVOKED',finished_at_ms=3,updated_at_ms=3 WHERE id=?1`,
		rpgSchemaOriginal)

	var launchState string
	var revokedAt int64
	err := database.QueryRowContext(t.Context(), `
SELECT launch.state,capability.revoked_at_ms
FROM launch_sessions launch
JOIN isolated_runtime_capabilities capability ON capability.launch_id=launch.id
WHERE launch.id=?1`, rpgSchemaOriginal).Scan(&launchState, &revokedAt)
	testassert.Falsef(t, err != nil, "query terminal Launch and capability: %v", err)
	testassert.Truef(t, launchState == "REVOKED", "Launch state = %q", launchState)
	testassert.Truef(t, revokedAt == 2, "capability revoked_at_ms = %d", revokedAt)
}

func openRPGMakerSchemaDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	testassert.Falsef(t, err != nil, "Open() error = %v", err)
	testassert.False(t, database.ReadOnly.Close() != nil, "close schema fixture read pool")
	return database.SQL
}

func insertRPGMakerProviderProjection(t *testing.T, database *sql.DB) {
	t.Helper()
	mustExecRPGSchema(t, database, `
INSERT INTO runtime_providers(
 provider_id,provider_version,provider_api_version,bundle_sha256,manifest_sha256,module_sha256,
 source,activated_at_ms
) VALUES(?1,'1.0.0',1,?2,?3,?4,'candidate',1)`, rpgSchemaProvider, rpgSchemaBundle,
		strings.Repeat("d", 64), strings.Repeat("c", 64))
	mustExecRPGSchema(t, database, `
INSERT INTO runtime_targets(
 provider_id,target_id,display_name,game_compatibility_line,options_kind,capabilities_json,
 checkpoint_json,manifest_fragment_json,target_contract_sha256
) VALUES(?1,?2,'RPG Maker 2000',?3,'RPGMAKER_V1','{}',
 '{"writeFormat":"checkpoint-v1","readFormats":["checkpoint-v1"],"maxBytes":67108864}','{}',?4)`,
		rpgSchemaProvider, rpgSchemaTarget, rpgSchemaGameLine, rpgSchemaTargetContract)
}

func preparePositionValidation(t *testing.T, database *sql.DB) {
	t.Helper()
	insertSchemaIdentity(t, database)
	insertRPGMakerProviderProjection(t, database)
	insertSchemaBlob(t, database, "checkpoint-blob", "a", 10)
	mustExecRPGSchema(t, database, `
INSERT INTO platform_instances(
 id,platform_id,default_core_id,name,slug,sort_order,enabled,version,created_at_ms,updated_at_ms,catalog_template_key
) VALUES('directory','rpgmaker','rpgmaker','RPG Maker','rpg-maker',1,1,1,1,1,
 'rpgmaker/rpgmaker')`)
	mustExecRPGSchema(t, database, `
INSERT INTO upload_sessions(
 id,purpose,state,source_type,total_files,total_bytes,manifest_digest,expires_at_ms,created_at_ms,updated_at_ms
) VALUES('upload','RPG_MAKER_PROJECT','COMPLETE','DIRECTORY',1,10,?1,1000000,1,1)`, strings.Repeat("1", 64))
	mustExecRPGSchema(t, database, `
INSERT INTO import_jobs(
 id,upload_session_id,target_platform_instance_id,platform_instance_version,platform_id,default_core_id,
 provider_id,target_id,target_contract_sha256,metadata_provider,config_snapshot_json,config_snapshot_digest,state,total_item_count,
 review_pending_item_count,created_at_ms,updated_at_ms
) VALUES('import','upload','directory',1,'rpgmaker','rpgmaker',?1,?2,?3,'NONE','{}',?4,
 'REVIEW_PENDING',1,1,1,1)`, rpgSchemaProvider, rpgSchemaTarget, rpgSchemaTargetContract, strings.Repeat("2", 64))
	mustExecRPGSchema(t, database, `
INSERT INTO import_items(
 id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,search_text,created_at_ms,updated_at_ms
) VALUES('item','import',?1,'REVIEW_PENDING','{}',?2,'fixture',1,1)`,
		strings.Repeat("3", 64), strings.Repeat("4", 64))
	manifest := `{"schemaVersion":2,"contentKind":"RPG_MAKER_PROJECT_V1","fileCount":1,"totalBytes":10,"filesDigest":"` +
		strings.Repeat("5", 64) + `"}`
	mustExecRPGSchema(t, database, `
INSERT INTO import_item_source_snapshots(
 id,import_item_id,revision_no,content_kind,source_manifest_json,source_manifest_digest,created_by,created_at_ms
) VALUES('snapshot','item',1,'RPG_MAKER_PROJECT_V1',?1,?2,'IDENTIFICATION',1)`,
		manifest, strings.Repeat("6", 64))
	mustExecRPGSchema(t, database, `
INSERT INTO review_drafts(
 id,import_item_id,target_platform_instance_id,metadata_json,runtime_binding_revision,version,
 created_at_ms,updated_at_ms,effective_source_snapshot_id
) VALUES('review','item','directory','{}',1,1,1,1,'snapshot')`)
	mustExecRPGSchema(t, database, `
INSERT INTO rpgmaker_review_profiles(
 review_draft_id,generation,evidence_family,evidence_generation,evidence_confidence,
 file_count,total_bytes,project_fingerprint,requirements_sha256,analysis_json,self_contained_override,
 provider_id,target_id,game_compatibility_line,target_contract_sha256,dependency_snapshot_sha256,
 created_at_ms,updated_at_ms
) VALUES('review','RPG2000','RPG2K','RPG2000','MATCHED',1,10,?1,?2,'{}',1,
 ?3,?4,?5,?6,?7,1,1)`, strings.Repeat("5", 64), strings.Repeat("7", 64),
		rpgSchemaProvider, rpgSchemaTarget, rpgSchemaGameLine, rpgSchemaTargetContract, strings.Repeat("8", 64))
	mustExecRPGSchema(t, database, `
INSERT INTO rpgmaker_runtime_validations(
 id,import_item_id,review_version_at_create,runtime_binding_revision,effective_source_snapshot_id,
 project_fingerprint,generation,evidence_generation,evidence_confidence,provider_id,target_id,
 game_compatibility_line,target_contract_sha256,dependency_snapshot_sha256,state,machine_gates_json,
 created_at_ms,updated_at_ms,expires_at_ms
) VALUES(?1,'item',1,1,'snapshot',?2,'RPG2000','RPG2000','MATCHED',?3,?4,?5,?6,?7,
 'CREATED','{}',1,1,900001)`, rpgSchemaValidation, strings.Repeat("5", 64),
		rpgSchemaProvider, rpgSchemaTarget, rpgSchemaGameLine, rpgSchemaTargetContract, strings.Repeat("8", 64))
	insertValidationLaunch(t, database, rpgSchemaOriginal, 1)
	mustExecRPGSchema(t, database, `UPDATE rpgmaker_runtime_validations SET launch_id=?1,state='STARTING' WHERE id=?2`,
		rpgSchemaOriginal, rpgSchemaValidation)
	mustExecRPGSchema(t, database, `UPDATE rpgmaker_runtime_validations SET state='RUNNING' WHERE id=?1`, rpgSchemaValidation)
	sequence := 1
	for _, gate := range []string{"RUNTIME_READY", "ENGINE_PROFILE", "FRAMES_300", "INPUT", "AUDIO"} {
		appendEmptyGatePair(t, database, &sequence, rpgSchemaOriginal, gate)
	}
	appendPositionGatePair(t, database, &sequence, rpgSchemaOriginal, "INITIAL_POSITION_RECORDED", 7, 10, 13, 0)
}

func insertValidationCheckpoint(t *testing.T, database *sql.DB) {
	t.Helper()
	mustExecRPGSchema(t, database, `
INSERT INTO rpgmaker_runtime_validation_checkpoints(
 validation_id,payload_blob_id,checkpoint_format,payload_sha256,size_bytes,created_at_ms
) VALUES(?1,'checkpoint-blob','checkpoint-v1',?2,10,2)`,
		rpgSchemaValidation, strings.Repeat("a", 64))
}

func attachSchemaRestoreLaunch(t *testing.T, database *sql.DB) {
	t.Helper()
	mustExecRPGSchema(t, database, `
UPDATE launch_sessions SET state='FINISHED',finished_at_ms=3,updated_at_ms=3 WHERE id=?1`, rpgSchemaOriginal)
	insertValidationLaunch(t, database, rpgSchemaRestore, 4)
	mustExecRPGSchema(t, database, `UPDATE rpgmaker_runtime_validations SET restore_launch_id=?1 WHERE id=?2`,
		rpgSchemaRestore, rpgSchemaValidation)
}

func insertValidationLaunch(t *testing.T, database *sql.DB, id string, createdAt int) {
	t.Helper()
	mustExecRPGSchema(t, database, `
INSERT INTO launch_sessions(
 id,profile_id,purpose,provider_id,target_id,target_contract_sha256,game_compatibility_line,bundle_sha256,effective_source_snapshot_id,
 rpgmaker_runtime_validation_id,return_to,credential_sha256,state,bootstrap_expires_at_ms,
 hard_expires_at_ms,created_at_ms,updated_at_ms
) VALUES(?1,'profile','RPG_RUNTIME_VALIDATION',?2,?3,?4,?5,?6,'snapshot',?7,
 '/admin/reviews',?8,'CREATED',?9,?10,?11,?11)`, id, rpgSchemaProvider, rpgSchemaTarget,
		rpgSchemaTargetContract, rpgSchemaGameLine, rpgSchemaBundle, rpgSchemaValidation,
		[]byte(strings.Repeat(id[:1], 32)), createdAt+60000, createdAt+900000, createdAt)
}

func appendPositionGatePair(
	t *testing.T,
	database *sql.DB,
	sequence *int,
	launchID, gate string,
	mapID, playerX, playerY, fixtureState int,
) {
	t.Helper()
	appendGateBegin(t, database, sequence, launchID, gate)
	appendPositionPass(t, database, sequence, launchID, gate, mapID, playerX, playerY, fixtureState)
}

func appendGateBegin(t *testing.T, database *sql.DB, sequence *int, launchID, gate string) {
	t.Helper()
	mustExecRPGSchema(t, database, `
INSERT INTO rpgmaker_runtime_validation_gate_events(
 validation_id,sequence,event_id,launch_id,gate,phase,observed_at_ms,evidence_json,created_at_ms
) VALUES(?1,?2,?3,?4,?5,'BEGIN',?2,'{}',?2)`, rpgSchemaValidation, *sequence,
		gate+"-begin", launchID, gate)
	advanceGateSequence(t, database, sequence)
}

func appendPositionPass(
	t *testing.T,
	database *sql.DB,
	sequence *int,
	launchID, gate string,
	mapID, playerX, playerY, fixtureState int,
) {
	t.Helper()
	evidence := positionEvidence(mapID, playerX, playerY, fixtureState)
	mustExecRPGSchema(t, database, `
INSERT INTO rpgmaker_runtime_validation_gate_events(
 validation_id,sequence,event_id,launch_id,gate,phase,observed_at_ms,evidence_json,created_at_ms
) VALUES(?1,?2,?3,?4,?5,'PASS',?2,?6,?2)`, rpgSchemaValidation, *sequence,
		gate+"-pass", launchID, gate, evidence)
	advanceGateSequence(t, database, sequence)
}

func appendEmptyGatePair(t *testing.T, database *sql.DB, sequence *int, launchID, gate string) {
	t.Helper()
	appendGateBegin(t, database, sequence, launchID, gate)
	mustExecRPGSchema(t, database, `
INSERT INTO rpgmaker_runtime_validation_gate_events(
 validation_id,sequence,event_id,launch_id,gate,phase,observed_at_ms,evidence_json,created_at_ms
) VALUES(?1,?2,?3,?4,?5,'PASS',?2,'{}',?2)`, rpgSchemaValidation, *sequence,
		gate+"-pass", launchID, gate)
	advanceGateSequence(t, database, sequence)
}

func advanceGateSequence(t *testing.T, database *sql.DB, sequence *int) {
	t.Helper()
	mustExecRPGSchema(t, database, `
UPDATE rpgmaker_runtime_validations SET last_gate_sequence=?1 WHERE id=?2`, *sequence, rpgSchemaValidation)
	*sequence++
}

func assertPositionPassRejected(
	t *testing.T,
	database *sql.DB,
	sequence int,
	launchID, gate string,
	playerX, fixtureState int,
) {
	t.Helper()
	_, err := database.ExecContext(context.Background(), `
INSERT INTO rpgmaker_runtime_validation_gate_events(
 validation_id,sequence,event_id,launch_id,gate,phase,observed_at_ms,evidence_json,created_at_ms
) VALUES(?,?,?,?,?,'PASS',?,?,?)`, rpgSchemaValidation, sequence, gate+"-rejected-"+strconv.Itoa(playerX),
		launchID, gate, sequence, positionEvidence(7, playerX, 13, fixtureState), sequence)
	testassert.Truef(t, err != nil && strings.Contains(err.Error(), "invalid runtime validation gate event"),
		"invalid %s evidence error = %v", gate, err)
}

func positionEvidence(mapID, playerX, playerY, fixtureState int) string {
	return `{"fixtureState":` + integerText(fixtureState) + `,"mapId":` + integerText(mapID) +
		`,"playerX":` + integerText(playerX) + `,"playerY":` + integerText(playerY) + `}`
}

func integerText(value int) string {
	return strconv.Itoa(value)
}

func insertSchemaIdentity(t *testing.T, database *sql.DB) {
	t.Helper()
	mustExecRPGSchema(t, database, `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('profile','Admin',1)`)
	mustExecRPGSchema(t, database, `
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES('user','profile','schema-admin','Schema Admin','ADMIN','ENABLED',1,1)`)
}

func insertSchemaBlob(t *testing.T, database *sql.DB, id, shaCharacter string, size int) {
	t.Helper()
	mustExecRPGSchema(t, database, `
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES(?,?,?,?,?,?,?,1)`, id, strings.Repeat(shaCharacter, 64), size, strings.Repeat("1", 32),
		strings.Repeat("2", 40), strings.Repeat("3", 8), "application/octet-stream")
}

func mustExecRPGSchema(t *testing.T, database *sql.DB, query string, arguments ...any) {
	t.Helper()
	_, err := database.ExecContext(context.Background(), query, arguments...)
	testassert.Falsef(t, err != nil, "schema fixture SQL failed: %v\n%s", err, query)
}
