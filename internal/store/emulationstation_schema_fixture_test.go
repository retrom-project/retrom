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
	testDigestA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testDigestB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testDigestC = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	testDigestD = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	testDigestE = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
)

type emulationStationSchemaFixture struct {
	database *DB
	importID string
	itemID   string
	userID   string
}

func openEmulationStationSchemaFixture(t *testing.T) emulationStationSchemaFixture {
	t.Helper()
	database, err := Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), func() time.Time {
		return time.UnixMilli(1_786_000_000_000)
	})
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	fixture := emulationStationSchemaFixture{
		database: database,
		importID: "00000000-0000-7000-8000-000000000001",
		itemID:   "00000000-0000-7000-8000-000000000004",
		userID:   "00000000-0000-7000-8000-000000000010",
	}
	fixture.seedIdentity(t)
	fixture.seedScanningImport(t)
	return fixture
}

func (fixture emulationStationSchemaFixture) seedIdentity(t *testing.T) {
	t.Helper()
	_, err := fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('profile-schema','Schema Admin',1);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'profile-schema','schema-admin','Schema Admin','ADMIN','ENABLED',1,1);
`, fixture.userID)
	testassert.False(t, err != nil, err)
}

func (fixture emulationStationSchemaFixture) seedScanningImport(t *testing.T) {
	t.Helper()
	_, err := fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO jobs(
 id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
 attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms
) VALUES(
 '00000000-0000-7000-8000-000000000002','EMULATIONSTATION_IMPORT',?,
 'SERVER_EMULATIONSTATION_SCAN',?,1,'{}',1,'QUEUED',0,4,1,1,1
)`, fixture.importID, testDigestA)
	testassert.False(t, err != nil, err)
	_, err = fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO emulationstation_imports(
 id,root_id,root_label_snapshot,source_relative_path,root_config_digest,release_year_max,
 state,phase,scan_job_id,created_by_user_id,created_at_ms,updated_at_ms,expires_at_ms
) VALUES(?,'roms','ROM root','fixture',?,2027,'SCANNING','DISCOVERING_GAMELISTS',
 '00000000-0000-7000-8000-000000000002',?,1,1,604800001);
`, fixture.importID, testDigestB, fixture.userID)
	testassert.False(t, err != nil, err)
	_, err = fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO emulationstation_import_gamelists(
 import_id,relative_path,size_bytes,content_digest,source_facts_digest,parse_state,error_code,
 game_count,folder_count,provider_present,ignored_fields_json,ignored_field_other_count,created_at_ms
) VALUES(?,'gamelist.xml',128,?,?,'VALID',NULL,1,0,0,'[]',0,1)
`, fixture.importID, testDigestC, testDigestD)
	testassert.False(t, err != nil, err)
	_, err = fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO emulationstation_import_collections(
 id,import_id,gamelist_relative_path,relative_directory,display_name,game_count,issue_count,
 folder_entry_count,hidden_game_count,adult_game_count,extension_summary_json,extension_other_count,
 created_at_ms,updated_at_ms
) VALUES('00000000-0000-7000-8000-000000000003',?,'gamelist.xml','','根目录',1,0,0,0,0,
 '[{"extension":".nes","count":1}]',0,1,1)
`, fixture.importID)
	testassert.False(t, err != nil, err)
	fixture.insertItem(t, fixture.itemID, 1, testDigestA,
		`{"hidden":false,"adult":false,"kidGame":false}`,
		validEmulationStationMetadataJSON(), `[]`, validEmulationStationManifestJSON(), false)
	_, err = fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO emulationstation_import_item_files(
 item_id,ordinal,declared_kind,relative_path,size_bytes,source_facts_digest,state,created_at_ms,updated_at_ms
) VALUES(?,0,'FILE','game.nes',16,?,'DISCOVERED',1,1)
`, fixture.itemID, testDigestE)
	testassert.False(t, err != nil, err)
}

func (fixture emulationStationSchemaFixture) insertItem(
	t *testing.T,
	id string,
	ordinal int,
	sourceKey, flags, metadata, warnings, manifest string,
	wantError bool,
) {
	t.Helper()
	_, err := fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO emulationstation_import_items(
 id,import_id,collection_id,gamelist_relative_path,game_ordinal,source_key,title,
 source_flags_json,discovery_state,execution_state,content_kind,metadata_json,warnings_json,
 source_manifest_json,source_manifest_digest,created_at_ms,updated_at_ms
) VALUES(?,?,'00000000-0000-7000-8000-000000000003','gamelist.xml',?,?,
 'Schema Game',?,'READY','PENDING','SINGLE_FILE',?,?,?, ?,1,1)
`, id, fixture.importID, ordinal, sourceKey, flags, metadata, warnings, manifest, testDigestB)
	if wantError {
		testassert.Truef(t, err != nil, "invalid EmulationStation item insert succeeded")
		return
	}
	testassert.False(t, err != nil, err)
}

func (fixture emulationStationSchemaFixture) finishScan(t *testing.T) {
	t.Helper()
	_, err := fixture.database.SQL.ExecContext(t.Context(), `
UPDATE emulationstation_imports
SET source_snapshot_digest=?,state='AWAITING_MAPPING',phase=NULL,
 gamelist_count=1,invalid_gamelist_count=0,collection_count=1,folder_entry_count=0,game_count=1,
 estimated_source_bytes=16,processable_item_count=1,blocked_item_count=0,scan_completed_at_ms=2,
 version=version+1,updated_at_ms=2
WHERE id=?
`, testDigestA, fixture.importID)
	testassert.False(t, err != nil, err)
}

func (fixture emulationStationSchemaFixture) seedLibraryReview(t *testing.T) (string, string) {
	t.Helper()
	jobID := "00000000-0000-7000-8000-000000000020"
	itemID := "00000000-0000-7000-8000-000000000022"
	_, err := fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO runtime_providers(
 provider_id,provider_version,provider_api_version,bundle_sha256,manifest_sha256,module_sha256,
 source,activated_at_ms
) VALUES('emulatorjs','1.0.0',1,?,?,?,'candidate',1)
`, testDigestA, testDigestB, testDigestC)
	testassert.False(t, err != nil, err)
	_, err = fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO runtime_targets(
 provider_id,target_id,display_name,game_compatibility_line,netplay_compatibility_line,
 target_options_schema_json,capabilities_json,checkpoint_json,manifest_fragment_json,target_contract_sha256
) VALUES('emulatorjs','fceumm','FCEUmm','fceumm-v1','emulatorjs-netplay-v2',
 '{"type":"object","additionalProperties":false,"properties":{},"required":[]}','{}',
 '{"writeFormat":"checkpoint-v1","readFormats":["checkpoint-v1"],"maxBytes":67108864}','{}',?)
`, testDigestD)
	testassert.False(t, err != nil, err)
	_, err = fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO platform_instances(
 id,platform_id,default_core_id,name,slug,sort_order,enabled,version,created_at_ms,updated_at_ms
) VALUES('platform-schema','nes','fceumm','Schema NES','schema-nes',1,1,1,1,1)
`)
	testassert.False(t, err != nil, err)
	_, err = fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO upload_sessions(
 id,state,source_type,total_files,total_bytes,manifest_digest,expires_at_ms,created_at_ms,updated_at_ms
) VALUES('upload-schema','COMPLETE','FILES',1,0,?,1000,1,1)
`, testDigestB)
	testassert.False(t, err != nil, err)
	_, err = fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO import_jobs(
 id,upload_session_id,target_platform_instance_id,platform_instance_version,platform_id,default_core_id,
 provider_id,target_id,target_contract_sha256,metadata_provider,config_snapshot_json,config_snapshot_digest,state,total_item_count,
 review_pending_item_count,created_at_ms,updated_at_ms
) VALUES(?,'upload-schema','platform-schema',1,'nes','fceumm','emulatorjs','fceumm',?,'NONE','{}',?,
 'REVIEW_PENDING',1,1,1,1)
`, jobID, testDigestD, testDigestC)
	testassert.False(t, err != nil, err)
	_, err = fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO import_items(
 id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,search_text,created_at_ms,updated_at_ms
) VALUES(?,?,?,'REVIEW_PENDING','[]',?,'schema game',1,1)
`, itemID, jobID, testDigestD, testDigestE)
	testassert.False(t, err != nil, err)
	return jobID, itemID
}

func (fixture emulationStationSchemaFixture) seedPegasusImport(t *testing.T) {
	t.Helper()
	_, err := fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO jobs(
 id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
 attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms
) VALUES('pegasus-scan','PEGASUS_IMPORT','pegasus-import','SERVER_PEGASUS_SCAN',?,1,'{}',1,'QUEUED',0,4,1,1,1)
`, testDigestA)
	testassert.False(t, err != nil, err)
	_, err = fixture.database.SQL.ExecContext(t.Context(), `
INSERT INTO pegasus_imports(
 id,root_id,root_label_snapshot,source_relative_path,root_config_digest,state,phase,scan_job_id,
 created_by_user_id,created_at_ms,updated_at_ms,expires_at_ms
) VALUES('pegasus-import','roms','ROM root','fixture',?,'SCANNING','DISCOVERING_METADATA','pegasus-scan',?,1,1,1000)
`, testDigestB, fixture.userID)
	testassert.False(t, err != nil, err)
}

func insertPegasusOwner(t *testing.T, database *sql.DB, libraryItemID string, wantError bool) {
	t.Helper()
	_, err := database.ExecContext(context.Background(), `
INSERT INTO pegasus_import_items(
 id,import_id,metadata_relative_path,game_ordinal,source_key,title,discovery_state,execution_state,
 content_kind,metadata_json,warnings_json,source_manifest_json,source_manifest_digest,
 library_import_item_id,created_at_ms,updated_at_ms
) VALUES('pegasus-item','pegasus-import','metadata.pegasus.txt',0,?,'Pegasus Game','READY','PENDING',
 'SINGLE_FILE','{}','[]','{}',?,?,1,1)
`, strings.Repeat("f", 64), testDigestA, libraryItemID)
	if wantError {
		testassert.Truef(t, err != nil, "cross-owned Pegasus item insert succeeded")
		return
	}
	testassert.False(t, err != nil, err)
}

func validEmulationStationMetadataJSON() string {
	return `{"schemaVersion":1,"title":"Schema Game","description":"","developer":"","publisher":"","genre":"","players":null,"releaseYear":null}`
}

func validEmulationStationManifestJSON() string {
	return `{"schemaVersion":1,"contentKind":"SINGLE_FILE","files":[{"ordinal":0,"declaredKind":"FILE","relativePath":"game.nes","sizeBytes":16,"sourceFactsDigest":"` + testDigestE + `"}]}`
}
