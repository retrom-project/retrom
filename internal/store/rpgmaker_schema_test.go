package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/testassert"
)

const (
	rpgSchemaProvider = "retrom-runtime"
	rpgSchemaTarget   = "rpgmaker-2000"
	rpgSchemaBundle   = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	rpgSchemaOriginal = "current-launch"
)

func TestRPGMakerProviderTargetSelectionAndCoreRouteAreDatabaseConstraints(t *testing.T) {
	t.Parallel()
	database := openRPGMakerSchemaDatabase(t)
	defer func() { cleanup.Error("close", database.Close()) }()
	insertRPGMakerProviderProjection(t, database)
	mustExecRPGSchema(t, database, `
INSERT INTO runtime_target_bindings(
 binding_id,core_id,provider_id,target_id,detector_profile,delivery_profile,launch_policy
) VALUES('retrom-runtime-rpgmaker-2000','rpgmaker',?1,?2,'RPG2000','FILE_TREE_PROJECT' ,'SUPPORTED')`,
		rpgSchemaProvider, rpgSchemaTarget)
	_, err := database.ExecContext(t.Context(), `
INSERT INTO runtime_target_bindings(
 binding_id,core_id,provider_id,target_id,detector_profile,delivery_profile,launch_policy
) VALUES('second-selected','fceumm',?1,?2,'RPG2000','FILE_TREE_PROJECT' ,'SUPPORTED')`,
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

func TestLaunchTerminationAcceptsCapabilityAlreadyRevokedByRuntimeCleanup(t *testing.T) {
	t.Parallel()
	database := openRPGMakerSchemaDatabase(t)
	defer func() { cleanup.Error("close", database.Close()) }()
	seedCurrentRuntimeGraph(t, database)
	mustExecRPGSchema(t, database, currentLaunchInsertSQL, rpgSchemaOriginal, "current-game-a", "target-a")
	// This test isolates the interaction between the capability's one-way revoke
	// trigger and the Launch terminal-state cascade. Ticket consumption itself is
	// covered by the isolation service tests.
	mustExecRPGSchema(t, database, `DROP TRIGGER isolated_runtime_capabilities_insert`)
	mustExecRPGSchema(t, database, `
INSERT INTO isolated_runtime_capabilities(
 credential_sha256,launch_id,profile_id,expected_origin,issued_at_ms,expires_at_ms
) VALUES(?1,?2,'current-profile','https://rpg-original-launch.rpg-runtime.example',1,100)`,
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
	seedSchemaProductDefinitions(t, database.SQL)
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
 provider_id,target_id,display_name,target_options_schema_json,capabilities_json,
 checkpoint_json,manifest_fragment_json
) VALUES(?1,?2,'RPG Maker 2000','{"type":"object","additionalProperties":false,"properties":{},"required":[]}','{}',
 '{"writeFormat":"checkpoint-v1","readFormats":["checkpoint-v1"],"maxBytes":67108864}','{}')`,
		rpgSchemaProvider, rpgSchemaTarget)
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
