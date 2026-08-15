package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"retrom/internal/cleanup"
)

func TestNetplayMigrationUpgradesVersion31AndEnforcesRoomInvariants(t *testing.T) {
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
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 1, 31)
	if _, err := legacy.ExecContext(ctx, `
INSERT INTO profiles(id,display_name,created_at_ms)
VALUES('01980000-0000-7000-8000-00000000a001','Netplay host',1)
`); err != nil {
		t.Fatal(err)
	}
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
	var version int
	if err := upgraded.SQL.QueryRow(`SELECT max(version) FROM schema_migrations`).Scan(&version); err != nil || version != 36 {
		t.Fatalf("schema version = %d, error=%v", version, err)
	}
	if _, err := upgraded.SQL.ExecContext(ctx, `
INSERT INTO netplay_rooms(id,host_profile_id,state,version,expires_at_ms,created_at_ms,updated_at_ms)
VALUES('01980000-0000-7000-8000-00000000c001','01980000-0000-7000-8000-00000000a001','DRAFT',1,901000,1000,1000);
INSERT INTO netplay_room_members(id,room_id,profile_id,role,player_no,ready,version,joined_at_ms,updated_at_ms)
VALUES('01980000-0000-7000-8000-00000000c002','01980000-0000-7000-8000-00000000c001',
'01980000-0000-7000-8000-00000000a001','HOST',1,0,1,1000,1000);
INSERT INTO netplay_events(room_id,profile_id,player_no,event_type,data_json,created_at_ms)
VALUES('01980000-0000-7000-8000-00000000c001','01980000-0000-7000-8000-00000000a001',1,
'ROOM_CREATED','{"schemaVersion":1}',1000)
`); err != nil {
		t.Fatal(err)
	}
	if _, err := upgraded.SQL.ExecContext(ctx, `
INSERT INTO netplay_rooms(id,host_profile_id,state,version,expires_at_ms,created_at_ms,updated_at_ms)
VALUES('01980000-0000-7000-8000-00000000c003','01980000-0000-7000-8000-00000000a001','DRAFT',1,902000,2000,2000)
`); err == nil {
		t.Fatal("a profile hosted two active rooms")
	}
	if _, err := upgraded.SQL.ExecContext(ctx, `UPDATE netplay_events SET data_json='{}' WHERE id=1`); err == nil {
		t.Fatal("append-only netplay event was updated")
	}
	if _, err := upgraded.SQL.ExecContext(ctx, `
UPDATE netplay_room_members SET ready=1,version=2,updated_at_ms=2000
WHERE id='01980000-0000-7000-8000-00000000c002'
`); err == nil {
		t.Fatal("DRAFT member was marked ready")
	}
	var saveAccessDefault string
	if err := upgraded.SQL.QueryRow(`
SELECT dflt_value FROM pragma_table_info('launch_sessions') WHERE name='save_access'
`).Scan(&saveAccessDefault); err != nil || saveAccessDefault != "'NORMAL'" {
		t.Fatalf("launch save_access default = %q, error=%v", saveAccessDefault, err)
	}
}
