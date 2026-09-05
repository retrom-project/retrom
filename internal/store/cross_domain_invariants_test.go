package store

import (
	"database/sql"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/testassert"
)

var (
	triggerRowReferencePattern  = regexp.MustCompile(`(?i)\b(?:NEW|OLD)\.([a-z_][a-z0-9_]*)`)
	triggerUpdateColumnsPattern = regexp.MustCompile(`(?is)\bUPDATE\s+OF\s+(.+?)\s+ON\s+[a-z_][a-z0-9_]*`)
)

func TestCurrentCrossDomainInvariantsContainNoLegacyCompatibilityIndexes(t *testing.T) {
	t.Parallel()
	database, err := Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	defer func() { cleanup.Error("close", database.Close()) }()

	names := queryStrings(t, database.SQL, `
SELECT name FROM sqlite_schema
WHERE name IN ('runtime_targets_game_compatibility','runtime_targets_netplay_compatibility',
               'game_variant_runtime_packs_immutable_update','game_variant_runtime_packs_immutable_delete')
ORDER BY name`)
	testassert.Truef(t, len(names) == 0, "legacy compatibility/current-state immutability objects remain: %v", names)
}

func TestCurrentCrossDomainTriggersReferenceExistingOwnerColumns(t *testing.T) {
	t.Parallel()
	database, err := Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	defer func() { cleanup.Error("close", database.Close()) }()

	rows, err := database.SQL.QueryContext(t.Context(), `
SELECT name,tbl_name,sql FROM sqlite_schema WHERE type='trigger' ORDER BY name`)
	testassert.False(t, err != nil, err)
	defer func() { testassert.False(t, rows.Close() != nil, "close trigger rows") }()
	type triggerSchema struct {
		name      string
		table     string
		statement string
	}
	triggers := make([]triggerSchema, 0)
	for rows.Next() {
		var trigger triggerSchema
		if err := rows.Scan(&trigger.name, &trigger.table, &trigger.statement); err != nil {
			t.Fatal(err)
		}
		triggers = append(triggers, trigger)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, trigger := range triggers {
		columns := tableColumns(t, database.SQL, trigger.table)
		if match := triggerUpdateColumnsPattern.FindStringSubmatch(trigger.statement); match != nil {
			for column := range strings.SplitSeq(match[1], ",") {
				name := strings.ToLower(strings.TrimSpace(column))
				testassert.Truef(t, columns[name],
					"trigger %s watches missing %s.%s", trigger.name, trigger.table, name)
			}
		}
		for _, match := range triggerRowReferencePattern.FindAllStringSubmatch(trigger.statement, -1) {
			testassert.Truef(t, columns[strings.ToLower(match[1])],
				"trigger %s references missing %s.%s", trigger.name, trigger.table, match[1])
		}
	}
}

func TestCurrentGameCanMoveBetweenPlatformInstances(t *testing.T) {
	t.Parallel()
	database, err := Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	defer func() { cleanup.Error("close", database.Close()) }()

	seedSchemaProductDefinitions(t, database.SQL)
	for _, statement := range []string{
		`INSERT INTO platform_instances(
 id,platform_id,default_core_id,name,slug,sort_order,enabled,version,created_at_ms,updated_at_ms
) VALUES('current-nes','nes','fceumm','Current NES','current-nes',1,1,1,1,1)`,
		`INSERT INTO platform_instances(
 id,platform_id,default_core_id,name,slug,sort_order,enabled,version,created_at_ms,updated_at_ms
) VALUES('current-snes','snes','snes9x','Current SNES','current-snes',2,1,1,1,1)`,
		`INSERT INTO games(
 id,platform_instance_id,title,title_initial,description,developer,publisher,genre,players,release_year,
 metadata_source_kind,metadata_source_ref_id,content_kind,content_source_kind,content_source_ref_id,
 source_manifest_json,source_manifest_digest,status,search_text,version,created_at_ms,updated_at_ms
) VALUES(
 'current-game','current-nes','Current game','C','','','','',1,NULL,
 'ADMIN_EDIT',NULL,'SINGLE_FILE','ADMIN_REPLACE','current-game-source',
 '{}','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','PUBLISHED','current game',1,1,1
)`,
	} {
		_, err := database.SQL.ExecContext(t.Context(), statement)
		testassert.False(t, err != nil, err)
	}

	_, err = database.SQL.ExecContext(t.Context(), `
UPDATE games
SET platform_instance_id='current-snes',version=version+1,updated_at_ms=2
WHERE id='current-game'`)
	testassert.Falsef(t, err != nil, "current game platform move was rejected: %v", err)

	var platformID string
	err = database.SQL.QueryRowContext(t.Context(), `SELECT platform_instance_id FROM games WHERE id='current-game'`).Scan(&platformID)
	testassert.False(t, err != nil, err)
	testassert.Truef(t, platformID == "current-snes", "current game platform = %s", platformID)
}

func TestCurrentSessionSnapshotsRejectForeignGameVariantAndLaunchOwners(t *testing.T) {
	t.Parallel()
	database, err := Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	defer func() { cleanup.Error("close", database.Close()) }()
	seedCurrentRuntimeGraph(t, database.SQL)

	_, err = database.SQL.ExecContext(t.Context(), `
INSERT INTO netplay_rooms(
 id,host_profile_id,state,selected_game_id,selected_game_variant_id,netplay_profile_id,
 profile_digest,max_players,expires_at_ms,created_at_ms,updated_at_ms
) VALUES(
 'foreign-room','current-profile','WAITING','current-game-a','current-variant-b','current-profile-v1',
 'dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',2,100,1,1
)`)
	testassert.Truef(t, err != nil && strings.Contains(err.Error(), "invalid netplay game variant"),
		"foreign netplay room variant error = %v", err)

	_, err = database.SQL.ExecContext(t.Context(), currentLaunchInsertSQL,
		"foreign-target-launch", "current-game-a", "target-b")
	testassert.Truef(t, err != nil && strings.Contains(err.Error(), "invalid runtime target snapshot"),
		"foreign product launch target error = %v", err)
	_, err = database.SQL.ExecContext(t.Context(), currentLaunchInsertSQL,
		"current-launch", "current-game-a", "target-a")
	testassert.False(t, err != nil, err)

	_, err = database.SQL.ExecContext(t.Context(), currentSaveInsertSQL, "foreign-save", "current-game-b")
	testassert.Truef(t, err != nil && strings.Contains(err.Error(), "invalid runtime checkpoint snapshot"),
		"foreign save source launch error = %v", err)
	_, err = database.SQL.ExecContext(t.Context(), currentSaveInsertSQL, "current-save", "current-game-a")
	testassert.False(t, err != nil, err)

	_, err = database.SQL.ExecContext(t.Context(), `
INSERT INTO netplay_rooms(
 id,host_profile_id,state,selected_game_id,selected_game_variant_id,netplay_profile_id,
 profile_digest,max_players,expires_at_ms,created_at_ms,updated_at_ms
) VALUES(
 'current-room','current-profile','WAITING','current-game-a','current-variant-a','current-profile-v1',
 'dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',2,100,1,1
)`)
	testassert.False(t, err != nil, err)
	_, err = database.SQL.ExecContext(t.Context(), currentNetplaySessionInsertSQL, "foreign-session", "target-b")
	testassert.Truef(t, err != nil && strings.Contains(err.Error(), "invalid netplay session snapshot"),
		"foreign netplay session target error = %v", err)
	_, err = database.SQL.ExecContext(t.Context(), currentNetplaySessionInsertSQL, "current-session", "target-a")
	testassert.False(t, err != nil, err)
}

const currentLaunchInsertSQL = `
INSERT INTO launch_sessions(
 id,profile_id,game_id,core_id,provider_id,target_id,bundle_sha256,content_kind,
 dependency_snapshot_json,compatibility_code,return_to,credential_sha256,state,
 bootstrap_expires_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms
) VALUES(
 ?,'current-profile',?,'fceumm','current-provider',?,
 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','SINGLE_FILE',
 '{}','READY','/',zeroblob(32),'CREATED',10,20,1,1
)`

const currentSaveInsertSQL = `
INSERT INTO save_states(
 id,profile_id,game_id,checkpoint_format,payload_blob_id,payload_sha256,payload_size_bytes,
 name,active_duration_ms,version,created_at_ms,updated_at_ms,source_launch_session_id
) VALUES(
 ?,'current-profile',?,'state-v1','current-save-payload',
 'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',1,
 'Current save',1,1,1,1,'current-launch'
)`

const currentNetplaySessionInsertSQL = `
INSERT INTO netplay_sessions(
 id,room_id,session_no,state,game_id,game_variant_id,provider_id,target_id,bundle_sha256,
 netplay_profile_id,profile_json,profile_digest,player_count,occupied_seat_mask,created_at_ms,updated_at_ms
) VALUES(
 ?,'current-room',1,'PREPARING','current-game-a','current-variant-a','current-provider',?,
 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','current-profile-v1','{}',
 'dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',2,3,1,1
)`

func seedCurrentRuntimeGraph(t *testing.T, database *sql.DB) {
	t.Helper()
	seedSchemaProductDefinitions(t, database)
	statements := []string{
		`INSERT INTO profiles(id,display_name,created_at_ms) VALUES('current-profile','Current profile',1)`,
		`INSERT INTO runtime_providers(
 provider_id,provider_version,provider_api_version,bundle_sha256,manifest_sha256,module_sha256,source,activated_at_ms
) VALUES(
 'current-provider','1.0.0',1,
 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
 'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc','candidate',1
)`,
		`INSERT INTO runtime_targets(
 provider_id,target_id,display_name,target_options_schema_json,capabilities_json,checkpoint_json,manifest_fragment_json
) VALUES
 ('current-provider','target-a','Target A','{"type":"object"}','{"netplayPort":true}',
  '{"writeFormat":"state-v1","readFormats":["state-v1"],"maxBytes":1024}','{}'),
 ('current-provider','target-b','Target B','{"type":"object"}','{"netplayPort":true}',
  '{"writeFormat":"state-v1","readFormats":["state-v1"],"maxBytes":1024}','{}')`,
		`INSERT INTO runtime_target_bindings(
 binding_id,core_id,provider_id,target_id,detector_profile,delivery_profile,launch_policy
) VALUES('current-binding','fceumm','current-provider','target-a','CURRENT','CURRENT','ENABLED')`,
		`INSERT INTO platform_instances(
 id,platform_id,default_core_id,name,slug,sort_order,enabled,version,created_at_ms,updated_at_ms
) VALUES('current-platform','nes','fceumm','Current platform','current-platform',1,1,1,1,1)`,
		`INSERT INTO games(
 id,platform_instance_id,title,title_initial,description,developer,publisher,genre,
 metadata_source_kind,content_kind,content_source_kind,content_source_ref_id,
 source_manifest_json,source_manifest_digest,status,search_text,created_at_ms,updated_at_ms
) VALUES
 ('current-game-a','current-platform','Current game A','C','','','','','ADMIN_EDIT','SINGLE_FILE',
  'ADMIN_REPLACE','current-source-a','{}','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
  'PUBLISHED','current game a',1,1),
 ('current-game-b','current-platform','Current game B','C','','','','','ADMIN_EDIT','SINGLE_FILE',
  'ADMIN_REPLACE','current-source-b','{}','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
  'PUBLISHED','current game b',1,1)`,
		`INSERT INTO game_variants(
 id,game_id,core_id,provider_id,target_id,status,compatibility_code,dependency_snapshot_json,created_at_ms,updated_at_ms
) VALUES
 ('current-variant-a','current-game-a','fceumm','current-provider','target-a','READY','READY','{}',1,1),
 ('current-variant-b','current-game-b','fceumm','current-provider','target-a','READY','READY','{}',1,1)`,
		`INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms) VALUES(
 'current-save-payload','cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',1,
 'dddddddddddddddddddddddddddddddd','eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee',
 'ffffffff','application/octet-stream',1
)`,
	}
	for _, statement := range statements {
		_, err := database.ExecContext(t.Context(), statement)
		testassert.False(t, err != nil, err)
	}
}
