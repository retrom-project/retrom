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

func TestReviewBulkMigrationUpgradesVersion36AndPreservesJobs(t *testing.T) {
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
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 1, 36)
	const jobID = "01980000-0000-7000-8000-000000000037"
	if _, err := legacy.ExecContext(ctx, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'BLOB','migration-037','BLOB_GC',?,1,'{}',1,'QUEUED',0,4,1,1,1,1)
`, jobID, strings.Repeat("3", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'BLOB','migration-037','QUEUED','{}',1)
`, jobID); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded := openHistoricalSchemaForTest(ctx, t, databasePath, repositoryRoot, func() time.Time { return time.UnixMilli(2_000) })
	defer func() { cleanup.Error("close", upgraded.Close()) }()
	if err := upgraded.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
	var version, jobs, events int
	if err := upgraded.SQL.QueryRow(`
SELECT (SELECT max(version) FROM schema_migrations),
       (SELECT count(*) FROM jobs WHERE id=?),
       (SELECT count(*) FROM job_events WHERE job_id=?)
`, jobID, jobID).Scan(&version, &jobs, &events); err != nil || version != 39 || jobs != 1 || events != 1 {
		t.Fatalf("upgrade = version:%d jobs:%d events:%d error:%v", version, jobs, events, err)
	}
	for _, name := range []string{
		"review_bulk_approvals", "review_bulk_approval_items",
		"review_bulk_approvals_frozen_update", "review_bulk_approval_items_owner_insert",
		"review_bulk_approval_items_frozen_update", "review_bulk_approval_items_published_update",
	} {
		var found int
		if err := upgraded.SQL.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name=?`, name).Scan(&found); err != nil || found != 1 {
			t.Fatalf("schema object %s = %d, %v", name, found, err)
		}
	}
	if _, err := upgraded.SQL.Exec(`
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES('01980000-0000-7000-8000-000000000038','IMPORT_ITEM','wrong','REVIEW_BULK_APPROVE',?,
1,'{}',1,'QUEUED',0,4,1,1,1,1)
`, strings.Repeat("4", 64)); err == nil {
		t.Fatal("REVIEW_BULK_APPROVE accepted the wrong scope")
	}
}
