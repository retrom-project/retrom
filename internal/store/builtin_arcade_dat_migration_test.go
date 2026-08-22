package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
)

func TestBuiltInArcadeDATMigrationRetiresUserCatalogManagement(t *testing.T) {
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
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 1, 37)
	seedLegacyUserDAT(t, legacy)
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(ctx, databasePath, func() time.Time { return time.UnixMilli(2_000) })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", upgraded.Close()) }()
	if err := upgraded.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
	var version, active int
	var parseStatus, jobState, cancelReason string
	if err := upgraded.SQL.QueryRow(`
SELECT (SELECT max(version) FROM schema_migrations),
       d.is_active,d.parse_status,j.state,j.cancel_reason
FROM dat_versions d
JOIN jobs j ON j.scope_id=d.id AND j.kind='DAT_PARSE'
WHERE d.id='legacy-user-dat'
`).Scan(&version, &active, &parseStatus, &jobState, &cancelReason); err != nil {
		t.Fatal(err)
	}
	if version != 39 || active != 0 || parseStatus != "CANCELLED" ||
		jobState != "CANCELLED" || cancelReason != "USER_DAT_RETIRED" {
		t.Fatalf("upgrade = version:%d active:%d DAT:%s job:%s reason:%s", version, active, parseStatus, jobState, cancelReason)
	}
	var legacyActive int
	if err := upgraded.SQL.QueryRow(`SELECT is_active FROM dat_versions WHERE id='legacy-active-user-dat'`).Scan(&legacyActive); err != nil || legacyActive != 0 {
		t.Fatalf("legacy active USER DAT = %d, %v", legacyActive, err)
	}
	for _, table := range []string{"dat_import_jobs", "dat_diff_snapshots", "dat_diff_items"} {
		var found int
		if err := upgraded.SQL.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&found); err != nil || found != 0 {
			t.Fatalf("retired table %s = %d, %v", table, found, err)
		}
	}
	if _, err := upgraded.SQL.Exec(`UPDATE dat_versions SET version=version+1 WHERE id='legacy-user-dat'`); err == nil {
		t.Fatal("legacy USER DAT remained mutable")
	}
	if _, err := upgraded.SQL.Exec(`
INSERT INTO dat_versions(id,core_id,core_artifact_id,source,builtin_relative_path,blob_id,sha256,
parser_version,compatibility_status,parse_status,is_active,version,created_at_ms,updated_at_ms)
VALUES('new-user-dat','fbneo','artifact','USER',NULL,'blob',?,'retrom-dat-v1','UNKNOWN','PENDING',0,1,1,1)
`, strings.Repeat("d", 64)); err == nil {
		t.Fatal("new USER DAT was accepted")
	}
}

func seedLegacyUserDAT(t *testing.T, database *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO core_artifacts(id,core_id,emulatorjs_version,bundle_version,flavor,relative_path,size_bytes,sha256,source_commit,provenance_json,compatibility_config_json,enabled,version,created_at_ms,updated_at_ms)
VALUES('artifact','fbneo','4.2.3','4.2.3','WASM','cores/fbneo.data',1,?,NULL,'{}','{}',1,1,1,1)`,
		`INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms) VALUES('blob',?,1,?,?,'00000000','application/xml',1)`,
		`INSERT INTO dat_versions(id,core_id,core_artifact_id,source,builtin_relative_path,blob_id,sha256,parser_version,compatibility_status,parse_status,is_active,version,created_at_ms,updated_at_ms)
VALUES('legacy-user-dat','fbneo','artifact','USER',NULL,'blob',?,'retrom-dat-v1','UNKNOWN','PENDING',0,1,1,1)`,
		`INSERT INTO dat_versions(id,core_id,core_artifact_id,source,builtin_relative_path,blob_id,sha256,parser_version,compatibility_status,parse_status,is_active,machine_count,rom_entry_count,disk_entry_count,bios_set_count,default_bios_set_count,explicit_bios_machine_count,base_dependency_target_count,unresolved_relation_count,version,created_at_ms,updated_at_ms,parsed_at_ms,activated_at_ms)
VALUES('legacy-active-user-dat','fbneo','artifact','USER',NULL,'blob',?,'retrom-dat-v1','USER_CONFIRMED','READY',1,0,0,0,0,0,0,0,0,1,1,1,1,1)`,
		`INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES('legacy-user-job','DAT_VERSION','legacy-user-dat','DAT_PARSE',?,1,'{}',1,'QUEUED',0,2,1,1,1,1)`,
		`INSERT INTO dat_import_jobs(job_id,dat_version_id,created_at_ms,updated_at_ms) VALUES('legacy-user-job','legacy-user-dat',1,1)`,
		`INSERT INTO dat_diff_snapshots(id,dat_version_id,state,input_digest,attempt_count,version,queued_at_ms,created_at_ms,updated_at_ms)
VALUES('legacy-user-diff','legacy-user-dat','PENDING',?,0,1,1,1,1)`,
	}
	arguments := [][]any{
		{strings.Repeat("a", 64)},
		{strings.Repeat("b", 64), strings.Repeat("c", 32), strings.Repeat("c", 40)},
		{strings.Repeat("b", 64)},
		{strings.Repeat("d", 64)},
		{strings.Repeat("e", 64)},
		{},
		{strings.Repeat("f", 64)},
	}
	for index, statement := range statements {
		if _, err := database.Exec(statement, arguments[index]...); err != nil {
			t.Fatalf("seed statement %d: %v", index, err)
		}
	}
}
