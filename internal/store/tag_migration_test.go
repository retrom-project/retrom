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

func TestTagMigrationUpgradesVersion33AndPreservesPegasusCollections(t *testing.T) {
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
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 1, 33)

	const (
		profileID    = "01980000-0000-7000-8000-00000000a434"
		adminID      = "01980000-0000-7000-8000-00000000b434"
		pegasusID    = "01980000-0000-7000-8000-00000000c434"
		scanJobID    = "01980000-0000-7000-8000-00000000d434"
		collectionID = "01980000-0000-7000-8000-00000000e434"
	)
	statements := []struct {
		query     string
		arguments []any
	}{
		{`INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,'Tag Migration Admin',1)`, []any{profileID}},
		{`INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,?,'tag.migration','Tag Migration Admin','ADMIN','ENABLED',1,1)`, []any{adminID, profileID}},
		{
			`INSERT INTO jobs(
  id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
  attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms
) VALUES(?,'PEGASUS_IMPORT',?,'SERVER_PEGASUS_SCAN',?,1,'{}',1,'QUEUED',0,4,1,1,1,1)`,
			[]any{scanJobID, pegasusID, strings.Repeat("4", 64)},
		},
		{
			`INSERT INTO pegasus_imports(
  id,root_id,root_label_snapshot,source_relative_path,root_config_digest,state,scan_job_id,
  collection_count,created_by_user_id,created_at_ms,updated_at_ms,scan_completed_at_ms,expires_at_ms
) VALUES(?,'games','Games','FC',?,'AWAITING_MAPPING',?,1,?,1,1,1,999999)`,
			[]any{pegasusID, strings.Repeat("5", 64), scanJobID, adminID},
		},
		{`INSERT INTO pegasus_import_collections(
  id,import_id,metadata_relative_path,segment_ordinal,name,game_count,created_at_ms,updated_at_ms
) VALUES(?,?,'FC/metadata.pegasus.txt',0,'FC',1,1,1)`, []any{collectionID, pegasusID}},
	}
	for index, statement := range statements {
		if _, err := legacy.ExecContext(ctx, statement.query, statement.arguments...); err != nil {
			t.Fatalf("fixture statement %d: %v", index, err)
		}
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
	var snapshot string
	if err := upgraded.SQL.QueryRow(`
SELECT (SELECT max(version) FROM schema_migrations),tag_snapshot_json
FROM pegasus_import_collections WHERE id=?
`, collectionID).Scan(&version, &snapshot); err != nil || version != 36 || snapshot != "[]" {
		t.Fatalf("upgrade = version:%d snapshot:%q error:%v", version, snapshot, err)
	}
	for _, table := range []string{"tags", "game_tags", "review_draft_tags", "pegasus_collection_tags"} {
		var found int
		if err := upgraded.SQL.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&found); err != nil || found != 1 {
			t.Fatalf("table %s = %d, %v", table, found, err)
		}
	}
}
