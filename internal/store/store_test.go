package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/migrations"
)

func TestMigrationsCreateIntegerBusinessTimesAndSeedCatalog(t *testing.T) {
	t.Parallel()
	database, err := Open(context.Background(), filepath.Join(t.TempDir(), "retrom.db"), func() time.Time {
		return time.UnixMilli(1786000000000)
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { cleanup.Error("close", database.Close()) }()
	if err := database.IntegrityCheck(context.Background()); err != nil {
		t.Fatalf("IntegrityCheck() error = %v", err)
	}

	rows, err := database.SQL.QueryContext(context.Background(), "SELECT name FROM sqlite_master WHERE type='table'")
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		if !strings.HasPrefix(name, "sqlite_") {
			tables = append(tables, name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate tables: %v", err)
	}
	for _, table := range tables {
		assertIntegerTimeColumns(t, database.SQL, table)
	}
	assertColumns(t, database.SQL, "review_preview_sessions",
		"import_item_id", "source_snapshot_id", "validation_id", "capture_allowed", "credential_sha256",
		"bootstrap_expires_at_ms", "hard_expires_at_ms")
	assertColumns(t, database.SQL, "review_preview_files", "preview_session_id", "role", "blob_id", "virtual_path")
	assertColumns(t, database.SQL, "review_runtime_screenshots",
		"import_item_id", "preview_session_id", "validation_id", "blob_id", "captured_after_ms", "captured_at_ms")
	var platformCount, coreCount, directoryCount int
	if err := database.SQL.QueryRow("SELECT (SELECT COUNT(*) FROM platforms), (SELECT COUNT(*) FROM cores), (SELECT COUNT(*) FROM platform_instances)").Scan(
		&platformCount,
		&coreCount,
		&directoryCount,
	); err != nil {
		t.Fatalf("count seed: %v", err)
	}
	if platformCount != 25 || coreCount != 35 || directoryCount != 29 {
		t.Fatalf("seed counts = %d/%d/%d", platformCount, coreCount, directoryCount)
	}
	var profileCount, userCount int
	var instanceState string
	if err := database.SQL.QueryRow(`
SELECT (SELECT count(*) FROM profiles),(SELECT count(*) FROM users),state FROM instance_state WHERE id=1
`).Scan(&profileCount, &userCount, &instanceState); err != nil {
		t.Fatal(err)
	}
	if profileCount != 0 || userCount != 0 || instanceState != "PENDING" {
		t.Fatalf("pending auth state = profiles:%d users:%d state:%s", profileCount, userCount, instanceState)
	}
	platformIDs := queryStrings(t, database.SQL, "SELECT id FROM platforms ORDER BY id")
	wantPlatforms := []string{"3do", "arcade", "atari2600", "atari5200", "atari7800", "dos", "fds", "gba", "gbc", "lynx", "mastersystem", "megadrive", "n64", "nds", "nes", "ngpc", "nintendo3ds", "pce", "pcfx", "psp", "psx", "saturn", "snes", "virtualboy", "wonderswan"}
	if !slices.Equal(platformIDs, wantPlatforms) {
		t.Fatalf("platform IDs = %#v", platformIDs)
	}
	coreIDs := queryStrings(t, database.SQL, "SELECT id FROM cores ORDER BY id")
	wantCores := []string{"a5200", "azahar", "beetle_vb", "desmume", "desmume2015", "dosbox_pure", "fbalpha2012_cps1", "fbalpha2012_cps2", "fbneo", "fceumm", "gambatte", "genesis_plus_gx", "genesis_plus_gx_wide", "handy", "mame2003", "mame2003_plus", "mednafen_ngp", "mednafen_pce", "mednafen_pcfx", "mednafen_psx_hw", "mednafen_wswan", "melonds", "mgba", "mupen64plus_next", "nestopia", "opera", "parallel_n64", "pcsx_rearmed", "picodrive", "ppsspp", "prosystem", "smsplus", "snes9x", "stella2014", "yabause"}
	if !slices.Equal(coreIDs, wantCores) {
		t.Fatalf("core IDs = %#v", coreIDs)
	}
	threadCores := queryStrings(t, database.SQL, "SELECT id FROM cores WHERE requires_threads=1 ORDER BY id")
	if !slices.Equal(threadCores, []string{"azahar", "dosbox_pure", "mednafen_psx_hw", "ppsspp"}) {
		t.Fatalf("thread cores = %#v", threadCores)
	}
	var enabledRelations int
	if err := database.SQL.QueryRow("SELECT count(*) FROM platform_cores WHERE enabled=1").Scan(&enabledRelations); err != nil || enabledRelations != 38 {
		t.Fatalf("enabled platform/core relations = %d, error=%v", enabledRelations, err)
	}
	relations := queryStrings(t, database.SQL, `
SELECT platform_id || ':' || core_id FROM platform_cores WHERE enabled=1 ORDER BY platform_id,core_id
`)
	wantRelations := []string{
		"3do:opera", "arcade:fbalpha2012_cps1", "arcade:fbalpha2012_cps2", "arcade:fbneo", "arcade:mame2003", "arcade:mame2003_plus",
		"atari2600:stella2014", "atari5200:a5200", "atari7800:prosystem", "dos:dosbox_pure",
		"fds:fceumm", "fds:nestopia", "gba:mgba", "gbc:gambatte", "gbc:mgba", "lynx:handy",
		"mastersystem:smsplus", "megadrive:genesis_plus_gx", "megadrive:genesis_plus_gx_wide", "megadrive:picodrive", "n64:mupen64plus_next", "n64:parallel_n64",
		"nds:desmume", "nds:desmume2015", "nds:melonds", "nes:fceumm", "nes:nestopia",
		"ngpc:mednafen_ngp", "nintendo3ds:azahar", "pce:mednafen_pce", "pcfx:mednafen_pcfx", "psp:ppsspp",
		"psx:mednafen_psx_hw", "psx:pcsx_rearmed", "saturn:yabause", "snes:snes9x",
		"virtualboy:beetle_vb", "wonderswan:mednafen_wswan",
	}
	if !slices.Equal(relations, wantRelations) {
		t.Fatalf("platform/core relations = %#v", relations)
	}
	instances := queryStrings(t, database.SQL, `
SELECT id || ':' || platform_id || ':' || default_core_id || ':' || slug || ':' || sort_order
FROM platform_instances ORDER BY id
`)
	wantInstances := []string{
		"01980000-0000-7000-8000-000000000001:nes:fceumm:nes-games:10",
		"01980000-0000-7000-8000-000000000002:fds:fceumm:fds-games:20",
		"01980000-0000-7000-8000-000000000003:snes:snes9x:snes-games:30",
		"01980000-0000-7000-8000-000000000004:gbc:gambatte:gbc-games:40",
		"01980000-0000-7000-8000-000000000005:gba:mgba:gba-games:50",
		"01980000-0000-7000-8000-000000000006:arcade:fbneo:fbneo-games:60",
		"01980000-0000-7000-8000-000000000007:arcade:mame2003_plus:mame2003-plus-games:70",
		"01980000-0000-7000-8000-000000000008:arcade:mame2003:mame2003-games:80",
		"01980000-0000-7000-8000-000000000009:dos:dosbox_pure:dos-games:90",
		"01980000-0000-7000-8000-000000000010:nds:desmume2015:nds-games:100",
		"01980000-0000-7000-8000-000000000011:atari2600:stella2014:atari-2600-games:110",
		"01980000-0000-7000-8000-000000000012:atari5200:a5200:atari-5200-games:120",
		"01980000-0000-7000-8000-000000000013:atari7800:prosystem:atari-7800-games:130",
		"01980000-0000-7000-8000-000000000014:lynx:handy:atari-lynx-games:140",
		"01980000-0000-7000-8000-000000000015:megadrive:genesis_plus_gx:mega-drive-games:150",
		"01980000-0000-7000-8000-000000000016:pce:mednafen_pce:pc-engine-games:160",
		"01980000-0000-7000-8000-000000000017:ngpc:mednafen_ngp:neo-geo-pocket-games:170",
		"01980000-0000-7000-8000-000000000018:n64:mupen64plus_next:nintendo-64-games:180",
		"01980000-0000-7000-8000-000000000019:psx:pcsx_rearmed:playstation-games:190",
		"01980000-0000-7000-8000-000000000020:saturn:yabause:sega-saturn-games:200",
		"01980000-0000-7000-8000-000000000021:pcfx:mednafen_pcfx:pc-fx-games:210",
		"01980000-0000-7000-8000-000000000022:3do:opera:3do-games:220",
		"01980000-0000-7000-8000-000000000023:psp:ppsspp:psp-games:230",
		"01980000-0000-7000-8000-000000000024:virtualboy:beetle_vb:virtual-boy-games:240",
		"01980000-0000-7000-8000-000000000025:wonderswan:mednafen_wswan:wonderswan-games:250",
		"01980000-0000-7000-8000-000000000026:mastersystem:smsplus:master-system-games:260",
		"01980000-0000-7000-8000-000000000027:arcade:fbalpha2012_cps1:fbalpha2012-cps1-games:270",
		"01980000-0000-7000-8000-000000000028:arcade:fbalpha2012_cps2:fbalpha2012-cps2-games:280",
		"01980000-0000-7000-8000-000000000029:nintendo3ds:azahar:nintendo-3ds-games:290",
	}
	if !slices.Equal(instances, wantInstances) {
		t.Fatalf("platform instances = %#v", instances)
	}
}

func queryStrings(t *testing.T, database *sql.DB, query string) []string {
	t.Helper()
	rows, err := database.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	values := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return values
}

func assertColumns(t *testing.T, database *sql.DB, table string, expected ...string) {
	t.Helper()
	rows, err := database.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	found := make(map[string]bool, len(expected))
	for rows.Next() {
		var id, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&id, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range expected {
		if !found[name] {
			t.Errorf("%s.%s is missing", table, name)
		}
	}
}

func applyMigrationRange(
	ctx context.Context,
	t *testing.T,
	database *sql.DB,
	repositoryRoot string,
	minimumVersion int,
	maximumVersion int,
) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(repositoryRoot, "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		version, parseErr := strconv.Atoi(strings.SplitN(entry.Name(), "_", 2)[0])
		if parseErr != nil || version < minimumVersion || version > maximumVersion {
			continue
		}
		migration, readErr := migrations.Files.ReadFile(entry.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		digest := sha256.Sum256(migration)
		if err := runMigration(
			ctx,
			database,
			version,
			entry.Name(),
			fmt.Sprintf("%x", digest),
			migration,
			time.Now,
		); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSupportedMigrationVersionsIdempotencyAndFutureProtection(t *testing.T) {
	t.Parallel()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	contents, err := os.ReadFile(filepath.Join(repositoryRoot, "migrations", "testdata", "supported_versions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var supported []int
	if err := json.Unmarshal(contents, &supported); err != nil || !slices.Equal(supported, []int{23, 24, 25}) {
		t.Fatalf("supported versions = %#v, error=%v", supported, err)
	}
	fixture := filepath.Join(repositoryRoot, "migrations", "testdata", "023_fixture.sql")
	if _, err := os.Stat(fixture); err != nil {
		t.Fatalf("migration fixture = %s, error=%v", fixture, err)
	}
	path := filepath.Join(t.TempDir(), "retrom.db")
	database, err := Open(context.Background(), path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err = Open(context.Background(), path, time.Now)
	if err != nil {
		t.Fatalf("second current-schema open: %v", err)
	}
	if _, err := database.SQL.Exec(`
INSERT INTO schema_migrations(version,
name,
checksum,
applied_at_ms) VALUES(999,
'future.sql',
?,
?)
`, strings.Repeat("0", 64), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if future, err := Open(context.Background(), path, time.Now); !errors.Is(err, ErrFutureSchema) {
		if future != nil {
			cleanup.Error("close", future.Close())
		}
		t.Fatalf("future schema error = %v", err)
	}
}

func TestMultiDiscMigrationUpgradesVersion23WithoutOwnershipDrift(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	databasePath := filepath.Join(t.TempDir(), "retrom.db")
	legacy, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	legacy.SetMaxOpenConns(1)
	if _, err := legacy.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, migrationTable); err != nil {
		t.Fatal(err)
	}
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 1, 23)
	fixture, err := os.ReadFile(filepath.Join(repositoryRoot, "migrations", "testdata", "023_fixture.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, string(fixture)); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(ctx, databasePath, func() time.Time { return time.UnixMilli(2000) })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", upgraded.Close()) }()
	if err := upgraded.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := upgraded.SQL.QueryRow(`SELECT max(version) FROM schema_migrations`).Scan(&version); err != nil || version != 31 {
		t.Fatalf("schema version = %d, error=%v", version, err)
	}
	var actorKind, actorUserID string
	if err := upgraded.SQL.QueryRow(`
SELECT actor_kind,actor_user_id FROM review_events
WHERE id='01980000-0000-7000-8000-00000000f006'
`).Scan(&actorKind, &actorUserID); err != nil || actorKind != "USER" ||
		actorUserID != "01980000-0000-7000-8000-00000000b001" {
		t.Fatalf("review actor = %s/%s, error=%v", actorKind, actorUserID, err)
	}
	var artifactVersion, generation int
	var validationDigest string
	if err := upgraded.SQL.QueryRow(`
SELECT core_artifact_version,prepublish_generation,prepublish_input_digest
FROM import_item_core_validations WHERE id='01980000-0000-7000-8000-00000000f004'
`).Scan(&artifactVersion, &generation, &validationDigest); err != nil || artifactVersion != 5 || generation != 3 ||
		validationDigest != strings.Repeat("3", 64) {
		t.Fatalf("historical validation = version:%d generation:%d digest:%s error=%v",
			artifactVersion, generation, validationDigest, err)
	}
	contentKinds := queryStrings(t, upgraded.SQL, `
SELECT id||':'||content_kind FROM import_item_source_snapshots
WHERE id IN ('01980000-0000-7000-8000-00000000f003','01980000-0000-7000-8000-00000000f030')
ORDER BY id
`)
	if !slices.Equal(contentKinds, []string{
		"01980000-0000-7000-8000-00000000f003:SINGLE_FILE",
		"01980000-0000-7000-8000-00000000f030:DOS_BUNDLE",
	}) {
		t.Fatalf("snapshot content kinds = %#v", contentKinds)
	}
	revisionKinds := queryStrings(t, upgraded.SQL, `
SELECT id||':'||content_kind FROM game_content_revisions
WHERE id IN ('01980000-0000-7000-8000-00000000f012','01980000-0000-7000-8000-00000000f015')
ORDER BY id
`)
	if !slices.Equal(revisionKinds, []string{
		"01980000-0000-7000-8000-00000000f012:SINGLE_FILE",
		"01980000-0000-7000-8000-00000000f015:DOS_BUNDLE",
	}) {
		t.Fatalf("revision content kinds = %#v", revisionKinds)
	}
	var userCount, saveOwnerCount, persistentOwnerCount, principalCount, parentAttachmentCount, multiAttachmentCount int
	if err := upgraded.SQL.QueryRow(`
SELECT
  (SELECT count(*) FROM users WHERE id IN (
    '01980000-0000-7000-8000-00000000b001','01980000-0000-7000-8000-00000000b002'
  )),
  (SELECT count(DISTINCT profile_id) FROM save_states WHERE disc_index IS NULL),
  (SELECT count(DISTINCT profile_id) FROM persistent_saves),
  (SELECT count(*) FROM idempotency_records WHERE principal_id='01980000-0000-7000-8000-00000000b001'),
  (SELECT count(*) FROM review_arcade_parent_attachments WHERE id='01980000-0000-7000-8000-00000000f008'),
  (SELECT count(*) FROM review_multidisc_attachments)
`).Scan(
		&userCount, &saveOwnerCount, &persistentOwnerCount, &principalCount, &parentAttachmentCount, &multiAttachmentCount,
	); err != nil {
		t.Fatal(err)
	}
	if userCount != 2 || saveOwnerCount != 2 || persistentOwnerCount != 2 || principalCount != 1 ||
		parentAttachmentCount != 1 || multiAttachmentCount != 0 {
		t.Fatalf(
			"preserved counts = users:%d saves:%d persistent:%d principal:%d parent:%d multi:%d",
			userCount, saveOwnerCount, persistentOwnerCount, principalCount, parentAttachmentCount, multiAttachmentCount,
		)
	}
	assertColumns(t, upgraded.SQL, "import_item_core_validations", "core_artifact_version", "prepublish_generation")
	assertColumns(t, upgraded.SQL, "launch_external_files", "kind")
	assertColumns(t, upgraded.SQL, "launch_sessions", "initial_disc_index")
	assertColumns(t, upgraded.SQL, "save_states", "disc_index")
	if _, err := upgraded.SQL.Exec(`
INSERT INTO import_item_source_snapshots(
id,import_item_id,revision_no,content_kind,source_manifest_json,source_manifest_digest,created_by,created_at_ms
) VALUES(
'01980000-0000-7000-8000-00000000f031','01980000-0000-7000-8000-00000000f002',3,
'MULTI_DISC_M3U_V1','[]','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1',
'IDENTIFICATION',2000
);
INSERT INTO import_item_source_snapshot_files(
source_snapshot_id,role,logical_name,upload_file_id,blob_id,sort_order,created_at_ms
) VALUES
('01980000-0000-7000-8000-00000000f031','PLAYLIST_SOURCE','playlist.m3u',
 '01980000-0000-7000-8000-00000000e002','01980000-0000-7000-8000-00000000d001',0,2000),
('01980000-0000-7000-8000-00000000f031','DISC','disc-a.chd',
 '01980000-0000-7000-8000-00000000e002','01980000-0000-7000-8000-00000000d001',0,2000);
INSERT INTO import_item_multidisc_entries(
source_snapshot_id,ordinal,source_reference,normalized_reference,canonical_name,state,
upload_file_id,blob_id,source_logical_name,created_at_ms
) VALUES
('01980000-0000-7000-8000-00000000f031',0,'Disc-A.CHD','disc-a.chd','disc-001.chd','PRESENT',
 '01980000-0000-7000-8000-00000000e002','01980000-0000-7000-8000-00000000d001','disc-a.chd',2000),
('01980000-0000-7000-8000-00000000f031',1,'Disc-B.CHD','disc-b.chd','disc-002.chd','MISSING',
 NULL,NULL,NULL,2000);
UPDATE review_drafts SET effective_source_snapshot_id='01980000-0000-7000-8000-00000000f031',
selected_validation_id=NULL,version=version+1,updated_at_ms=2000
WHERE id='01980000-0000-7000-8000-00000000f005';
INSERT INTO upload_sessions(
id,state,source_type,total_files,total_bytes,manifest_digest,version,expires_at_ms,created_at_ms,updated_at_ms
) VALUES(
'01980000-0000-7000-8000-00000000f032','COMPLETE','FILES',1,8,
'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb1',1,100000,2000,2000
);
INSERT INTO jobs(
id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,attempt_count,
max_attempts,version,available_at_ms,created_at_ms,updated_at_ms
) VALUES(
'01980000-0000-7000-8000-00000000f033','IMPORT_ITEM','01980000-0000-7000-8000-00000000f002',
'REVIEW_MULTI_DISC_VALIDATE','ccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc1',
1,'{}',1,'QUEUED',0,4,1,2000,2000,2000
);
INSERT INTO review_multidisc_attachments(
id,import_item_id,review_draft_id,requested_by_user_id,base_source_snapshot_id,upload_session_id,
expected_set_digest,state,diagnostics_json,job_id,version,created_at_ms,updated_at_ms
) VALUES(
'01980000-0000-7000-8000-00000000f034','01980000-0000-7000-8000-00000000f002',
'01980000-0000-7000-8000-00000000f005','01980000-0000-7000-8000-00000000b001',
'01980000-0000-7000-8000-00000000f031','01980000-0000-7000-8000-00000000f032',
'ddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd1','QUEUED','{}',
'01980000-0000-7000-8000-00000000f033',1,2000,2000
)
`); err != nil {
		t.Fatalf("insert multi-disc schema fixture: %v", err)
	}
	invalidStatements := []string{
		`INSERT INTO import_item_multidisc_entries(
source_snapshot_id,ordinal,source_reference,normalized_reference,canonical_name,state,created_at_ms
) VALUES(
'01980000-0000-7000-8000-00000000f031',1,'Other.CHD','other.chd','disc-002.chd','MISSING',2000
)`,
		`INSERT INTO review_multidisc_attachments(
id,import_item_id,review_draft_id,requested_by_user_id,base_source_snapshot_id,upload_session_id,
expected_set_digest,state,diagnostics_json,job_id,version,created_at_ms,updated_at_ms
) VALUES(
'01980000-0000-7000-8000-00000000f035','01980000-0000-7000-8000-00000000f002',
'01980000-0000-7000-8000-00000000f005','01980000-0000-7000-8000-00000000b001',
'01980000-0000-7000-8000-00000000f031','01980000-0000-7000-8000-00000000f032',
'eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee1','QUEUED','{}',
'01980000-0000-7000-8000-00000000f033',1,2000,2000
)`,
		`UPDATE review_multidisc_attachments SET requested_by_user_id='01980000-0000-7000-8000-00000000b002'
WHERE id='01980000-0000-7000-8000-00000000f034'`,
		`UPDATE review_multidisc_attachments SET state='ACCEPTED',version=2,updated_at_ms=2001
WHERE id='01980000-0000-7000-8000-00000000f034'`,
		`DELETE FROM import_item_multidisc_entries
WHERE source_snapshot_id='01980000-0000-7000-8000-00000000f031' AND ordinal=0`,
	}
	for _, statement := range invalidStatements {
		if _, err := upgraded.SQL.Exec(statement); err == nil {
			t.Fatalf("invalid multi-disc statement succeeded: %s", statement)
		}
	}
}

func TestMultiDiscMigrationRejectsMixedHistoricalLayoutWithoutWriting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	databasePath := filepath.Join(t.TempDir(), "retrom.db")
	legacy, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	legacy.SetMaxOpenConns(1)
	if _, err := legacy.ExecContext(ctx, migrationTable); err != nil {
		t.Fatal(err)
	}
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 1, 23)
	fixture, err := os.ReadFile(filepath.Join(repositoryRoot, "migrations", "testdata", "023_fixture.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, string(fixture)); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, `
INSERT INTO import_item_source_snapshot_files(
source_snapshot_id,role,logical_name,upload_file_id,blob_id,sort_order,created_at_ms
) VALUES(
'01980000-0000-7000-8000-00000000f030','COMPANION','mixed.bin',
'01980000-0000-7000-8000-00000000e002','01980000-0000-7000-8000-00000000d001',1,1000
)
`); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	if database, err := Open(ctx, databasePath, time.Now); err == nil {
		cleanup.Error("close", database.Close())
		t.Fatal("mixed schema upgrade unexpectedly succeeded")
	}
	readonly, err := sql.Open("sqlite", "file:"+filepath.ToSlash(databasePath)+"?mode=ro&immutable=1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", readonly.Close()) }()
	var maximum int
	if err := readonly.QueryRow(`SELECT max(version) FROM schema_migrations`).Scan(&maximum); err != nil || maximum != 23 {
		t.Fatalf("schema version after rejected migration = %d, error=%v", maximum, err)
	}
	var newColumnCount int
	if err := readonly.QueryRow(`
SELECT count(*) FROM pragma_table_info('import_item_source_snapshots') WHERE name='content_kind'
`).Scan(&newColumnCount); err != nil || newColumnCount != 0 {
		t.Fatalf("new column after rejected migration = %d, error=%v", newColumnCount, err)
	}
}

func TestVersion19DatabaseIsRejectedBeforeAccountOrMultiDiscWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	databasePath := filepath.Join(t.TempDir(), "retrom.db")
	legacy, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	legacy.SetMaxOpenConns(1)
	if _, err := legacy.ExecContext(ctx, migrationTable); err != nil {
		t.Fatal(err)
	}
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 1, 19)
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	if database, err := Open(ctx, databasePath, time.Now); !errors.Is(err, ErrDatabaseRebuild) {
		if database != nil {
			cleanup.Error("close", database.Close())
		}
		t.Fatalf("version 19 startup error = %v", err)
	}
	readonly, err := sql.Open("sqlite", "file:"+filepath.ToSlash(databasePath)+"?mode=ro&immutable=1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", readonly.Close()) }()
	var maximum, userTables, multiTables int
	if err := readonly.QueryRow(`
SELECT
  (SELECT max(version) FROM schema_migrations),
  (SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='users'),
  (SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='review_multidisc_attachments')
`).Scan(&maximum, &userTables, &multiTables); err != nil {
		t.Fatal(err)
	}
	if maximum != 19 || userTables != 0 || multiTables != 0 {
		t.Fatalf("version 19 changed = max:%d users:%d multi:%d", maximum, userTables, multiTables)
	}
}

func TestReviewArcadeParentMigrationUpgradesVersion18(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	databasePath := filepath.Join(t.TempDir(), "retrom.db")
	legacy, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	legacy.SetMaxOpenConns(1)
	if _, err := legacy.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, migrationTable); err != nil {
		t.Fatal(err)
	}
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 1, 18)
	fixture, err := os.ReadFile(filepath.Join(repositoryRoot, "migrations", "testdata", "018_fixture.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, string(fixture)); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(ctx, databasePath, time.Now)
	if !errors.Is(err, ErrDatabaseRebuild) {
		t.Fatalf("legacy version 18 error = %v", err)
	}
	if upgraded != nil {
		cleanup.Error("close", upgraded.Close())
	}
}

func TestReviewArcadeParentMigrationRejectsDriftedManifest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	databasePath := filepath.Join(t.TempDir(), "retrom.db")
	legacy, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	legacy.SetMaxOpenConns(1)
	if _, err := legacy.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, migrationTable); err != nil {
		t.Fatal(err)
	}
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 1, 18)
	fixture, err := os.ReadFile(filepath.Join(repositoryRoot, "migrations", "testdata", "018_fixture.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, string(fixture)); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, `UPDATE import_items SET source_manifest_json='[]' WHERE id='v18-item'`); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	if upgraded, err := Open(ctx, databasePath, time.Now); !errors.Is(err, ErrDatabaseRebuild) {
		if upgraded != nil {
			cleanup.Error("close", upgraded.Close())
		}
		t.Fatalf("drifted manifest error = %v", err)
	}
}

func TestDOSExternalConfigMigrationRepointsLegacyLaunchContent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	databasePath := filepath.Join(t.TempDir(), "retrom.db")
	legacy, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	legacy.SetMaxOpenConns(1)
	if _, err := legacy.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, migrationTable); err != nil {
		t.Fatal(err)
	}
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 1, 10)
	fixture, err := os.ReadFile(filepath.Join(repositoryRoot, "migrations", "testdata", "010_fixture.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, string(fixture)); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(ctx, databasePath, time.Now)
	if !errors.Is(err, ErrDatabaseRebuild) {
		t.Fatalf("legacy version 10 error = %v", err)
	}
	if upgraded != nil {
		cleanup.Error("close", upgraded.Close())
	}
}

func TestCoreExpansionMigrationPreservesArchiveReferences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	databasePath := filepath.Join(t.TempDir(), "retrom.db")
	legacy, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	legacy.SetMaxOpenConns(1)
	if _, err := legacy.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, migrationTable); err != nil {
		t.Fatal(err)
	}
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 1, 10)
	fixture, err := os.ReadFile(filepath.Join(repositoryRoot, "migrations", "testdata", "010_fixture.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, string(fixture)); err != nil {
		t.Fatal(err)
	}
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 11, 13)
	if _, err := legacy.ExecContext(ctx, `
INSERT INTO archive_entries(
  archive_blob_id,ordinal,original_relative_path,normalized_path,ascii_casefold_path,
  archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,materialized_blob_id,created_at_ms
) VALUES(
  'base-blob',0,'fixture.rom','fixture.rom','fixture.rom','ZIP','DEFLATE',1024,
  'aaaaaaaa','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
  'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','derived-blob',1
);
INSERT INTO upload_sessions(
  id,state,source_type,total_files,total_bytes,manifest_digest,expires_at_ms,created_at_ms,updated_at_ms
) VALUES(
  'archive-upload','COMPLETE','FILES',1,1024,
  'ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',1000,1,1
);
INSERT INTO upload_files(
  id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,final_blob_id,state,created_at_ms,updated_at_ms
) VALUES('archive-file','archive-upload','fixture.zip',1024,1024,'base-blob','COMPLETE',1,1);
INSERT INTO import_jobs(
  id,upload_session_id,target_platform_instance_id,platform_instance_version,platform_id,default_core_id,
  core_artifact_id,metadata_provider,config_snapshot_json,config_snapshot_digest,state,total_item_count,
  review_pending_item_count,created_at_ms,updated_at_ms
) VALUES(
  'archive-import','archive-upload','01980000-0000-7000-8000-000000000009',1,'dos','dosbox_pure',
  'dos-artifact','NONE','{}','ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',
  'REVIEW_PENDING',1,1,1,1
);
INSERT INTO import_items(
  id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,search_text,created_at_ms,updated_at_ms
) VALUES(
  'archive-item','archive-import','ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',
  'REVIEW_PENDING','[{"blobSha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","logicalName":"fixture.rom","role":"CONTENT","sizeBytes":1030,"sourceArchiveEntryOrdinal":0,"sourceArchiveSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]',
  'c3bde87793bbc3a4d8f41645317379954d23aa01589087321f04a449dfc7b1aa','fixture',1,1
);
INSERT INTO import_item_source_files(
  import_item_id,role,logical_name,upload_file_id,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order,created_at_ms
) VALUES('archive-item','CONTENT','fixture.rom','archive-file','derived-blob','base-blob',0,0,1);
INSERT INTO game_content_files(
  game_content_revision_id,role,logical_name,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order
) VALUES('dos-content','CONTENT','fixture.rom','derived-blob','base-blob',0,0);
INSERT INTO jobs(
  id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,attempt_count,max_attempts,
  available_at_ms,execution_started_at_ms,finished_at_ms,created_at_ms,updated_at_ms
) VALUES(
  'scrape-job','IMPORT_ITEM','archive-item','METADATA_SCRAPE',
  'ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',1,'{}',1,'SUCCEEDED',1,4,1,1,1,1,1
);
INSERT INTO metadata_scrape_runs(
  id,import_item_id,job_id,provider,provider_config_version,state,created_at_ms,updated_at_ms,completed_at_ms
) VALUES('scrape-run','archive-item','scrape-job','NONE',1,'COMPLETED',1,1,1);
INSERT INTO content_hash_evidence(
  id,scrape_run_id,profile,archive_blob_id,archive_entry_ordinal,crc32,query_order,created_at_ms
) VALUES('archive-evidence','scrape-run','SINGLE_ARCHIVE_MEMBER_V1','base-blob',0,'aaaaaaaa',0,1);
`); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(ctx, databasePath, time.Now)
	if !errors.Is(err, ErrDatabaseRebuild) {
		t.Fatalf("legacy version 13 error = %v", err)
	}
	if upgraded != nil {
		cleanup.Error("close", upgraded.Close())
	}
}

func TestImportProgressRepairMigrationFinalizesZeroItemJobs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	databasePath := filepath.Join(t.TempDir(), "retrom.db")
	legacy, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	legacy.SetMaxOpenConns(1)
	if _, err := legacy.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, migrationTable); err != nil {
		t.Fatal(err)
	}
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 1, 10)
	fixture, err := os.ReadFile(filepath.Join(repositoryRoot, "migrations", "testdata", "010_fixture.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, string(fixture)); err != nil {
		t.Fatal(err)
	}
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 11, 14)
	if _, err := legacy.ExecContext(ctx, `
INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,expires_at_ms,created_at_ms,updated_at_ms)
VALUES
  ('rejected-upload','COMPLETE','FILES',1,1,'ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',1000,1,1),
  ('ignored-upload','COMPLETE','FILES',1,1,'eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee',1000,1,1);
INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,platform_id,default_core_id,
core_artifact_id,metadata_provider,config_snapshot_json,config_snapshot_digest,state,total_item_count,rejected_file_count,created_at_ms,updated_at_ms)
VALUES
  ('rejected-import','rejected-upload','01980000-0000-7000-8000-000000000009',1,'dos','dosbox_pure','dos-artifact','HASHEOUS','{}','ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff','RUNNING',0,1,1,1),
  ('ignored-import','ignored-upload','01980000-0000-7000-8000-000000000009',1,'dos','dosbox_pure','dos-artifact','NONE','{}','eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee','REVIEW_PENDING',0,0,1,1);
`); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(ctx, databasePath, time.Now)
	if !errors.Is(err, ErrDatabaseRebuild) {
		t.Fatalf("legacy version 14 error = %v", err)
	}
	if upgraded != nil {
		cleanup.Error("close", upgraded.Close())
	}
}

func assertIntegerTimeColumns(t *testing.T, database *sql.DB, table string) {
	t.Helper()
	rows, err := database.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("inspect %s: %v", table, err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan %s column: %v", table, err)
		}
		if strings.HasSuffix(name, "_at_ms") && strings.ToUpper(dataType) != "INTEGER" {
			t.Errorf("%s.%s uses %s, want INTEGER", table, name, dataType)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s columns: %v", table, err)
	}
}
