package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/testassert"
)

func TestImportReviewAggregateMigrationRepairsDiscardedTerminalJob(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	databasePath := filepath.Join(t.TempDir(), "retrom.db")
	legacy, err := sql.Open("sqlite", databasePath)
	testassert.False(t, err != nil, err)
	legacy.SetMaxOpenConns(1)
	if _, err := legacy.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, migrationTable); err != nil {
		t.Fatal(err)
	}
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 1, 23)
	fixture, err := os.ReadFile(filepath.Join(repositoryRoot, "migrations", "testdata", "023_fixture.sql"))
	testassert.False(t, err != nil, err)
	if _, err := legacy.ExecContext(ctx, string(fixture)); err != nil {
		t.Fatal(err)
	}
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 24, 35)
	if _, err := legacy.ExecContext(ctx, `
UPDATE import_items
SET state='DISCARDED',version=version+1,updated_at_ms=1500,completed_at_ms=1500
WHERE id='01980000-0000-7000-8000-00000000f002'
`); err != nil {
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
	var schemaVersion, pending, published, discarded, jobVersion int64
	var completedAt sql.NullInt64
	var jobState, itemState string
	if err := upgraded.SQL.QueryRowContext(context.Background(), `
SELECT (SELECT max(version) FROM schema_migrations),
job.state,
job.review_pending_item_count,
job.published_item_count,
job.discarded_item_count,
job.version,
job.completed_at_ms,
item.state
FROM import_jobs job
JOIN import_items item ON item.import_job_id=job.id
WHERE job.id='01980000-0000-7000-8000-00000000f001'
`).Scan(
		&schemaVersion,
		&jobState,
		&pending,
		&published,
		&discarded,
		&jobVersion,
		&completedAt,
		&itemState,
	); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return schemaVersion != 39 }, func() bool { return jobState != "COMPLETED" }, func() bool { return pending != 0 }, func() bool { return published != 0 }, func() bool { return discarded != 1 }, func() bool { return jobVersion != 2 }, func() bool { return !completedAt.Valid }, func() bool { return completedAt.Int64 != 1500 }, func() bool { return itemState != "DISCARDED" }), "aggregate repair = schema:%d job:%s pending:%d published:%d discarded:%d version:%d completed:%d item:%s", schemaVersion, jobState, pending, published, discarded, jobVersion, completedAt.Int64, itemState)
}
