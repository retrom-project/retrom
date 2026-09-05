package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/testassert"
)

func TestMigrationsCreateCurrentSchemaWithoutProductSeeds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "retrom.db"), func() time.Time {
		return time.UnixMilli(1786000000000)
	})
	testassert.Falsef(t, err != nil, "Open() error = %v", err)
	defer func() { cleanup.Error("close", database.Close()) }()
	testassert.Falsef(t, database.IntegrityCheck(ctx) != nil, "fresh database integrity failed")

	tables := queryStrings(t, database.SQL, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	assertColumns(t, database.SQL, "import_group_requests",
		"import_job_id", "request_digest", "actor_user_id", "upload_version",
		"upload_manifest_digest", "target_snapshot_digest")
	testassert.Truef(t, slices.Equal(queryStrings(t, database.SQL, `
SELECT name FROM sqlite_master
WHERE type='trigger' AND name LIKE 'import_group_requests_immutable_%' ORDER BY name
`), []string{"import_group_requests_immutable_delete", "import_group_requests_immutable_update"}),
		"import group request immutability triggers drifted")
	for _, table := range tables {
		assertIntegerTimeColumns(t, database.SQL, table)
	}
	testassert.Falsef(t, len(tables) != 122, "fresh schema table count = %d", len(tables))
	assertColumns(t, database.SQL, "review_preview_sessions",
		"import_item_id", "source_snapshot_id", "validation_id", "credential_sha256",
		"bootstrap_expires_at_ms", "hard_expires_at_ms", "checkpoint_payload_blob_id", "checkpoint_format",
		"restore_from_preview_id", "restore_payload_blob_id", "restore_checkpoint_format")
	assertColumns(t, database.SQL, "review_preview_files", "preview_session_id", "role", "blob_id", "virtual_path")
	assertColumns(t, database.SQL, "review_runtime_screenshots",
		"import_item_id", "preview_session_id", "validation_id", "blob_id", "captured_at_ms")
	for _, removed := range []struct{ table, column string }{
		{"review_preview_sessions", "capture_allowed"}, {"review_runtime_screenshots", "captured_after_ms"},
	} {
		var found int
		if err := database.SQL.QueryRowContext(t.Context(), "SELECT count(*) FROM pragma_table_info(?) WHERE name=?", removed.table, removed.column).Scan(&found); err != nil {
			t.Fatal(err)
		}
		if found != 0 {
			t.Fatalf("obsolete review screenshot policy remains: %s.%s", removed.table, removed.column)
		}
	}
	assertColumns(t, database.SQL, "netplay_rooms", "host_profile_id", "state", "profile_digest", "expires_at_ms")
	assertColumns(t, database.SQL, "netplay_room_members", "room_id", "profile_id", "player_no", "ready")
	assertColumns(t, database.SQL, "netplay_sessions", "provider_id", "target_id", "bundle_sha256",
		"profile_json", "occupied_seat_mask", "resync_count")
	assertColumns(t, database.SQL, "netplay_session_participants", "credential_sha256", "lease_expires_at_ms")
	assertColumns(t, database.SQL, "netplay_events", "event_type", "data_json", "created_at_ms")
	assertColumns(t, database.SQL, "launch_sessions", "netplay_session_id", "netplay_player_no", "save_access")
	assertColumns(t, database.SQL, "launch_sessions", "game_id", "core_id", "provider_id",
		"target_id", "bundle_sha256", "content_kind", "dependency_snapshot_json", "compatibility_code")
	assertColumns(t, database.SQL, "runtime_providers", "provider_version", "provider_api_version",
		"bundle_sha256", "manifest_sha256", "module_sha256", "source")
	assertColumns(t, database.SQL, "runtime_targets", "target_id", "target_options_schema_json",
		"capabilities_json", "checkpoint_json")
	assertColumns(t, database.SQL, "runtime_target_bindings", "core_id", "provider_id", "target_id",
		"detector_profile", "delivery_profile", "launch_policy")
	assertColumns(t, database.SQL, "save_states", "game_id", "checkpoint_format", "payload_blob_id",
		"payload_sha256", "payload_size_bytes", "source_launch_session_id")
	assertColumns(t, database.SQL, "isolated_runtime_bootstrap_tickets", "launch_id", "preview_id")
	assertColumns(t, database.SQL, "isolated_runtime_capabilities", "launch_id", "preview_id")
	assertColumns(t, database.SQL, "runtime_asset_pack_installations", "status", "version", "validated_at_ms")
	assertNotNullColumn(t, database.SQL, "games", "title_initial")
	assertNotNullColumn(t, database.SQL, "save_states", "source_launch_session_id")
	assertColumns(t, database.SQL, "dat_versions", "provider_id", "target_id",
		"builtin_relative_path", "sha256", "parser_version", "parse_status")
	assertColumns(t, database.SQL, "review_bulk_approvals", "source_flagged_count")

	var platformCount, coreCount, relationCount, directoryCount int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM platforms),
       (SELECT count(*) FROM cores),
       (SELECT count(*) FROM platform_cores WHERE enabled=1),
       (SELECT count(*) FROM platform_instances WHERE deleted_at_ms IS NULL)
`).Scan(&platformCount, &coreCount, &relationCount, &directoryCount); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(
		func() bool { return platformCount != 0 },
		func() bool { return coreCount != 0 },
		func() bool { return relationCount != 0 },
		func() bool { return directoryCount != 0 },
	), "seed counts = %d/%d/%d/%d", platformCount, coreCount, relationCount, directoryCount)
	assertColumns(t, database.SQL, "platform_instances", "catalog_template_key")

	var profileCount, userCount int
	var instanceState string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM profiles),(SELECT count(*) FROM users),state FROM instance_state WHERE id=1
`).Scan(&profileCount, &userCount, &instanceState); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(
		func() bool { return profileCount != 0 },
		func() bool { return userCount != 0 },
		func() bool { return instanceState != "PENDING" },
	), "pending auth state = profiles:%d users:%d state:%s", profileCount, userCount, instanceState)

	testassert.Falsef(t, tableColumns(t, database.SQL, "cores")["requires_threads"], "thread capability remained on cores")
	assertCurrentClosedEnums(t, database.SQL)
}

func TestSchemaUsesCurrentGameStateWithoutBusinessRevisions(t *testing.T) {
	t.Parallel()
	database, err := Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	defer func() { cleanup.Error("close", database.Close()) }()

	tables := queryStrings(t, database.SQL, `
SELECT name FROM sqlite_master
WHERE type='table' AND name IN ('game_content_revisions','game_metadata_revisions','game_variant_revisions')
ORDER BY name`)
	testassert.Truef(t, len(tables) == 0, "business revision tables still exist: %v", tables)
	assertColumns(t, database.SQL, "games",
		"title", "description", "developer", "publisher", "genre", "content_kind",
		"source_manifest_json", "source_manifest_digest", "version")
	assertColumns(t, database.SQL, "game_files", "game_id", "role", "logical_name", "blob_id", "sort_order")
	assertColumns(t, database.SQL, "game_variants",
		"game_id", "core_id", "provider_id", "target_id", "status", "dependency_snapshot_json", "version")
	for table := range map[string]struct{}{"save_states": {}, "launch_sessions": {}, "play_sessions": {}} {
		columns := tableColumns(t, database.SQL, table)
		for _, forbidden := range []string{"game_content_revision_id", "game_variant_revision_id"} {
			testassert.Falsef(t, columns[forbidden], "%s still contains %s", table, forbidden)
		}
	}
}

func TestGameMetadataTitleInitialConstraint(t *testing.T) {
	t.Parallel()
	database, err := Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	testassert.Falsef(t, err != nil, "Open() error = %v", err)
	defer func() { cleanup.Error("close", database.Close()) }()
	transaction, err := database.SQL.BeginTx(t.Context(), nil)
	testassert.False(t, err != nil, err)
	defer func() { cleanup.Error("rollback", transaction.Rollback()) }()
	if _, err := transaction.ExecContext(t.Context(), "PRAGMA defer_foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	insert := func(id, initial string) error {
		_, insertErr := transaction.ExecContext(t.Context(), `
INSERT INTO games(
 id,platform_instance_id,title,title_initial,description,developer,publisher,genre,
 metadata_source_kind,content_source_kind,content_source_ref_id,source_manifest_json,
 source_manifest_digest,status,search_text,created_at_ms,updated_at_ms
) VALUES(?,'missing-instance','Game',?,'','','','',
 'ADMIN_EDIT','ADMIN_REPLACE','fixture','{}',?,'PUBLISHED','game',1,1)
`, id, initial, strings.Repeat("a", 64))
		return insertErr
	}
	for index, initial := range []string{"#", "0", "9", "A", "Z"} {
		if err := insert(fmt.Sprintf("valid-%d", index), initial); err != nil {
			t.Fatalf("valid title_initial %q rejected: %v", initial, err)
		}
	}
	for index, initial := range []string{"", "a", "AA", "中", "É"} {
		err := insert(fmt.Sprintf("invalid-%d", index), initial)
		testassert.Truef(t, err != nil && strings.Contains(err.Error(), "CHECK constraint failed"),
			"invalid title_initial %q error = %v", initial, err)
	}
}

func assertCurrentClosedEnums(t *testing.T, database *sql.DB) {
	t.Helper()
	for table, current := range map[string]string{
		"emulationstation_imports":      "phase TEXT CHECK(phase IS NULL OR phase IN ('DISCOVERING_GAMELISTS','PARSING_GAMELISTS','RESOLVING_SOURCES','COPYING_CONTENT','VALIDATING','PREPARING_REVIEWS'))",
		"emulationstation_import_items": "execution_state TEXT NOT NULL CHECK(execution_state IN ('PENDING','COPYING','VALIDATING','REVIEW_PENDING','PUBLISHED','REVIEW_DISCARDED','SKIPPED_EXISTING','SKIPPED_MAPPING','BLOCKED_SOURCE','BLOCKED_CONTENT','SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED','CANCELLED'))",
		"pegasus_imports":               "phase TEXT CHECK(phase IS NULL OR phase IN ('DISCOVERING_METADATA','PARSING_METADATA','RESOLVING_SOURCES','COPYING_CONTENT','VALIDATING','PREPARING_REVIEWS'))",
		"pegasus_import_items":          "execution_state TEXT NOT NULL CHECK(execution_state IN ('PENDING','COPYING','VALIDATING','REVIEW_PENDING','PUBLISHED','REVIEW_DISCARDED','SKIPPED_EXISTING','SKIPPED_MAPPING','BLOCKED_SOURCE','BLOCKED_CONTENT','SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED','CANCELLED'))",
		"upload_consumptions":           "consumer_type TEXT NOT NULL CHECK(consumer_type IN ( 'IMPORT_JOB','GAME_CONTENT_REPLACE_JOB','GAME_ASSET','REVIEW_ASSET','REVIEW_ARCADE_PARENT', 'REVIEW_MULTI_DISC','BIOS_INSTALLATION','RUNTIME_ASSET_PACK_INSTALLATION' ))",
	} {
		var source string
		if err := database.QueryRowContext(t.Context(), "SELECT sql FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&source); err != nil {
			t.Fatal(err)
		}
		testassert.Truef(t, strings.Contains(strings.Join(strings.Fields(source), " "), current), "%s lacks current closed enum", table)
	}
}

func TestOpenProvidesIndependentConfiguredReadPool(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	testassert.Falsef(t, err != nil, "Open() error = %v", err)
	defer func() { cleanup.Error("close", database.Close()) }()
	testassert.Falsef(t, testassert.Any(
		func() bool { return database.ReadOnly == nil },
		func() bool { return database.ReadOnly.Stats().MaxOpenConnections != 4 },
	), "read pool is not independently configured")

	writer, err := database.SQL.Conn(ctx)
	testassert.Falsef(t, err != nil, "reserve writer connection: %v", err)
	defer func() { cleanup.Error("close", writer.Close()) }()
	readContext, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var foreignKeys, busyTimeout, tableCount int
	if err := database.ReadOnly.QueryRowContext(readContext, `
SELECT (SELECT foreign_keys FROM pragma_foreign_keys),
       (SELECT timeout FROM pragma_busy_timeout),
       (SELECT count(*) FROM sqlite_schema WHERE type='table')
`).Scan(&foreignKeys, &busyTimeout, &tableCount); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(
		func() bool { return foreignKeys != 1 },
		func() bool { return busyTimeout != 5000 },
		func() bool { return tableCount == 0 },
	), "read pool configuration = foreign_keys:%d busy_timeout:%d tables:%d", foreignKeys, busyTimeout, tableCount)
}

func TestCurrentMigrationLineageResumeAndReopen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "retrom.db")
	sources, err := migrationSources()
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, len(sources) != 10, "migration count = %d", len(sources))
	database := openMigrationTestDatabase(t, path)
	for _, source := range sources[:len(sources)-1] {
		if err := runMigration(ctx, database, source, time.Now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES('archive',?,1,?,?,?,'application/zip',1)
`, strings.Repeat("a", 64), strings.Repeat("b", 32), strings.Repeat("c", 40), strings.Repeat("d", 8)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO archive_entries(
 archive_blob_id,ordinal,original_relative_path,normalized_path,ascii_casefold_path,
 archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,created_at_ms
) VALUES('archive',0,'index.html','index.html','index.html','ZIP','DEFLATE',1,?,?,?,?,1)
`, strings.Repeat("d", 8), strings.Repeat("b", 32), strings.Repeat("c", 40), strings.Repeat("e", 64)); err != nil {
		t.Fatal(err)
	}
	testassert.False(t, database.Close() != nil, "close prefix database")

	resumed, err := Open(ctx, path, time.Now)
	testassert.Falsef(t, err != nil, "resume current prefix: %v", err)
	var maximum int
	if err := resumed.SQL.QueryRowContext(ctx, "SELECT max(version) FROM schema_migrations").Scan(&maximum); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, maximum != len(sources), "resumed version = %d", maximum)
	if _, err := resumed.SQL.ExecContext(ctx, `
INSERT INTO archive_entries(
 archive_blob_id,ordinal,original_relative_path,normalized_path,ascii_casefold_path,
 archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,created_at_ms
) VALUES('archive',1,'data/start.ks','data/start.ks','data/start.ks',
         'ELECTRON_ASAR','ELECTRON_ASAR_DEFLATE',1,?,?,?,?,2)
`, strings.Repeat("d", 8), strings.Repeat("b", 32), strings.Repeat("c", 40), strings.Repeat("f", 64)); err != nil {
		t.Fatal(err)
	}
	var archiveEntries int
	if err := resumed.SQL.QueryRowContext(ctx, `
SELECT count(*) FROM archive_entries WHERE archive_blob_id='archive'
`).Scan(&archiveEntries); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, archiveEntries != 2, "migrated archive entry count = %d", archiveEntries)
	testassert.False(t, resumed.Close() != nil, "close resumed database")

	reopened, err := Open(ctx, path, time.Now)
	testassert.Falsef(t, err != nil, "idempotent reopen: %v", err)
	testassert.False(t, reopened.Close() != nil, "close reopened database")
}

func TestFreshSchemaUsesProviderTargetProjectionWithoutLegacyRuntimeSemantics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retrom.db")
	createCurrentDatabase(t, path)
	database, err := sql.Open("sqlite", path)
	testassert.False(t, err != nil, err)
	defer func() { cleanup.Error("close", database.Close()) }()

	rows, err := database.QueryContext(t.Context(), `
SELECT name,sql FROM sqlite_schema WHERE sql IS NOT NULL ORDER BY name
	`)
	testassert.False(t, err != nil, err)
	defer func() { cleanup.Error("close schema rows", rows.Close()) }()
	forbidden := []string{
		"route_key", "runtime_family", "runtime_adapter_kind", "adapter_abi", "save_abi",
		"payload_kind", "native_profile", "resume_slot", "core_artifact_id", "core_artifacts",
	}
	foundTables := map[string]bool{}
	for rows.Next() {
		var name, statement string
		if err := rows.Scan(&name, &statement); err != nil {
			t.Fatal(err)
		}
		foundTables[name] = true
		lowered := strings.ToLower(statement)
		for _, legacy := range forbidden {
			if strings.Contains(lowered, legacy) {
				t.Fatalf("schema object %s retains %q: %s", name, legacy, statement)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"runtime_providers", "runtime_targets", "runtime_catalog_state"} {
		if !foundTables[required] {
			t.Fatalf("missing Provider projection table %s", required)
		}
	}
	var syntheticRPGCores int
	if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM cores WHERE id LIKE 'rpgmaker_%'`).Scan(&syntheticRPGCores); err != nil {
		t.Fatal(err)
	}
	if syntheticRPGCores != 0 {
		t.Fatalf("synthetic RPG generation cores = %d", syntheticRPGCores)
	}
}

func TestMigrationPreflightRejectsOldCorruptGapAndFutureLineages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tests := []struct {
		name  string
		setup func(*testing.T, string)
		want  error
	}{
		{name: "old lineage", setup: func(t *testing.T, path string) {
			database := openMigrationTestDatabase(t, path)
			_, err := database.ExecContext(t.Context(), `CREATE TABLE old_marker(id INTEGER PRIMARY KEY);
INSERT INTO schema_migrations(version,name,checksum,applied_at_ms) VALUES(1,'001_platform.sql',?,1)`, strings.Repeat("0", 64))
			testassert.False(t, err != nil, err)
			testassert.False(t, database.Close() != nil, "close old lineage")
		}, want: ErrDatabaseRebuild},
		{name: "checksum mismatch", setup: func(t *testing.T, path string) {
			createCurrentDatabase(t, path)
			database, err := sql.Open("sqlite", path)
			testassert.False(t, err != nil, err)
			_, err = database.ExecContext(t.Context(), "UPDATE schema_migrations SET checksum=? WHERE version=1", strings.Repeat("0", 64))
			testassert.False(t, err != nil, err)
			testassert.False(t, database.Close() != nil, "close checksum database")
		}, want: ErrMigrationChecksum},
		{name: "gap", setup: func(t *testing.T, path string) {
			sources, err := migrationSources()
			testassert.False(t, err != nil, err)
			database := openMigrationTestDatabase(t, path)
			for _, source := range []migrationSource{sources[0], sources[2]} {
				_, err := database.ExecContext(t.Context(), "INSERT INTO schema_migrations(version,name,checksum,applied_at_ms) VALUES(?,?,?,1)", source.version, source.name, source.checksum)
				testassert.False(t, err != nil, err)
			}
			testassert.False(t, database.Close() != nil, "close gap database")
		}, want: ErrSchemaInvalid},
		{name: "future", setup: func(t *testing.T, path string) {
			createCurrentDatabase(t, path)
			database, err := sql.Open("sqlite", path)
			testassert.False(t, err != nil, err)
			_, err = database.ExecContext(t.Context(), "INSERT INTO schema_migrations(version,name,checksum,applied_at_ms) VALUES(999,'999_future.sql',?,1)", strings.Repeat("0", 64))
			testassert.False(t, err != nil, err)
			testassert.False(t, database.Close() != nil, "close future database")
		}, want: ErrFutureSchema},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "retrom.db")
			test.setup(t, path)
			opened, err := Open(ctx, path, time.Now)
			if opened != nil {
				cleanup.Error("close", opened.Close())
			}
			testassert.Truef(t, errors.Is(err, test.want), "Open() error = %v, want %v", err, test.want)
			if test.name == "old lineage" {
				readOnly, openErr := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
				testassert.False(t, openErr != nil, openErr)
				var tableCount int
				if err := readOnly.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tableCount); err != nil {
					t.Fatal(err)
				}
				testassert.Falsef(t, tableCount != 2, "old lineage was modified; table count = %d", tableCount)
				cleanup.Error("close", readOnly.Close())
			}
		})
	}
}

func TestFailedMigrationRollsBackSchemaAndCatalog(t *testing.T) {
	t.Parallel()
	database := openMigrationTestDatabase(t, filepath.Join(t.TempDir(), "retrom.db"))
	defer func() { cleanup.Error("close", database.Close()) }()
	contents := []byte("CREATE TABLE partial_write(id INTEGER PRIMARY KEY); INVALID SQL;")
	digest := sha256.Sum256(contents)
	source := migrationSource{version: 1, name: "001_broken.sql", checksum: fmt.Sprintf("%x", digest), contents: contents}
	testassert.Truef(t, runMigration(context.Background(), database, source, time.Now) != nil, "broken migration succeeded")
	var tableCount, recordCount int
	if err := database.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='partial_write'").Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(t.Context(), "SELECT count(*) FROM schema_migrations").Scan(&recordCount); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, tableCount != 0 || recordCount != 0, "failed migration leaked table=%d record=%d", tableCount, recordCount)
}

func createCurrentDatabase(t *testing.T, path string) {
	t.Helper()
	database, err := Open(context.Background(), path, time.Now)
	testassert.False(t, err != nil, err)
	testassert.False(t, database.Close() != nil, "close current database")
}

func openMigrationTestDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", path)
	testassert.False(t, err != nil, err)
	database.SetMaxOpenConns(1)
	_, err = database.ExecContext(t.Context(), "PRAGMA foreign_keys=ON;"+migrationTable)
	if err != nil {
		cleanup.Error("close", database.Close())
		t.Fatal(err)
	}
	return database
}

func queryStrings(t *testing.T, database *sql.DB, query string) []string {
	t.Helper()
	rows, err := database.QueryContext(t.Context(), query)
	testassert.False(t, err != nil, err)
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
	found := tableColumns(t, database, table)
	for _, name := range expected {
		testassert.CheckTruef(t, found[name], "%s.%s is missing", table, name)
	}
}

func assertNotNullColumn(t *testing.T, database *sql.DB, table, column string) {
	t.Helper()
	rows, err := database.QueryContext(t.Context(), `PRAGMA table_info(`+table+`)`)
	testassert.False(t, err != nil, err)
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var id, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&id, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == column {
			testassert.Truef(t, notNull == 1, "%s.%s must be NOT NULL", table, column)
			return
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatalf("%s.%s is missing", table, column)
}

func tableColumns(t *testing.T, database *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := database.QueryContext(t.Context(), `PRAGMA table_info(`+table+`)`)
	testassert.False(t, err != nil, err)
	defer func() { cleanup.Error("close", rows.Close()) }()
	found := make(map[string]bool)
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
	return found
}

func assertIntegerTimeColumns(t *testing.T, database *sql.DB, table string) {
	t.Helper()
	rows, err := database.QueryContext(t.Context(), `PRAGMA table_info(`+table+`)`)
	testassert.False(t, err != nil, err)
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var id, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&id, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "_at_ms") {
			testassert.CheckTruef(t, strings.EqualFold(dataType, "INTEGER"), "%s.%s type = %s", table, name, dataType)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
