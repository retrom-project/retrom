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

func TestServerImportMigrationUpgradesVersion25WithoutJobEventLoss(t *testing.T) {
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
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 1, 25)
	if _, err := legacy.ExecContext(ctx, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,finished_at_ms,created_at_ms,updated_at_ms)
VALUES('01980000-0000-7000-8000-00000000d001','DAT_VERSION','fixture','DAT_PARSE',?,1,'{}',1,
'SUCCEEDED',1,4,1,1,2,1,2);
INSERT INTO job_events(id,job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(9001,'01980000-0000-7000-8000-00000000d001','DAT_VERSION','fixture','SUCCEEDED','{}',2);
`, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(ctx, databasePath, func() time.Time { return time.UnixMilli(3000) })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", upgraded.Close()) }()
	if err := upgraded.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
	var jobs, events, version int
	if err := upgraded.SQL.QueryRow(`SELECT
(SELECT count(*) FROM jobs WHERE id='01980000-0000-7000-8000-00000000d001'),
(SELECT count(*) FROM job_events WHERE id=9001),
(SELECT max(version) FROM schema_migrations)`).Scan(&jobs, &events, &version); err != nil || jobs != 1 || events != 1 || version != 32 {
		t.Fatalf("upgrade result = jobs:%d events:%d version:%d error:%v", jobs, events, version, err)
	}
	if _, err := upgraded.SQL.Exec(`
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('01980000-0000-7000-8000-00000000a001','Admin',1);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES('01980000-0000-7000-8000-00000000b001','01980000-0000-7000-8000-00000000a001','migration.admin','Admin','ADMIN','ENABLED',1,1);
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES('01980000-0000-7000-8000-00000000d002','SERVER_IMPORT','01980000-0000-7000-8000-00000000e001',
'SERVER_BIOS_IMPORT',?,1,'{}',1,'QUEUED',0,4,1,1,1,1);
INSERT INTO server_imports(id,kind,root_id,root_label_snapshot,source_relative_path,root_config_digest,
catalog_snapshot_digest,replace_if_better,state,catalog_item_count,job_id,created_by_user_id,version,created_at_ms,updated_at_ms)
VALUES('01980000-0000-7000-8000-00000000e001','BIOS_DIRECTORY','bios-root','BIOS root','',?,?,0,
'QUEUED',0,'01980000-0000-7000-8000-00000000d002','01980000-0000-7000-8000-00000000b001',1,1,1)
`, strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := upgraded.SQL.Exec(`UPDATE server_imports SET state='FAILED',completed_at_ms=2,failed_item_count=1 WHERE id='01980000-0000-7000-8000-00000000e001'`); err == nil {
		t.Fatal("terminal classification count greater than catalog was accepted")
	}
	if _, err := upgraded.SQL.Exec(`INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms) VALUES('01980000-0000-7000-8000-00000000d003','DAT_VERSION','wrong','SERVER_BIOS_IMPORT',?,1,'{}',1,'QUEUED',0,4,1,1,1,1)`, strings.Repeat("e", 64)); err == nil {
		t.Fatal("SERVER_BIOS_IMPORT with a non-server scope was accepted")
	}
}
