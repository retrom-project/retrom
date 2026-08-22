package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"runtime"
	"testing"

	_ "modernc.org/sqlite"

	"retrom/internal/cleanup"
	"retrom/internal/testassert"
)

func TestPlatformDirectoryConsolidationMovesGamesAndRetainsHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "consolidation.db"))
	testassert.False(t, err != nil, err)
	defer func() { cleanup.Error("close", database.Close()) }()
	database.SetMaxOpenConns(1)
	if _, err := database.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, migrationTable); err != nil {
		t.Fatal(err)
	}
	applyMigrationRange(ctx, t, database, repositoryRoot, 1, 38)

	transaction, err := database.BeginTx(ctx, nil)
	testassert.False(t, err != nil, err)
	if _, err := transaction.ExecContext(ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		gameID      string
		metadataID  string
		contentID   string
		directoryID string
		logicalName string
	}{
		{
			gameID: "01980000-0000-7000-8000-00000000a101", metadataID: "01980000-0000-7000-8000-00000000a102",
			contentID: "01980000-0000-7000-8000-00000000a103", directoryID: "01980000-0000-7000-8000-000000000002", logicalName: "disk.fds",
		},
		{
			gameID: "01980000-0000-7000-8000-00000000a201", metadataID: "01980000-0000-7000-8000-00000000a202",
			contentID: "01980000-0000-7000-8000-00000000a203", directoryID: "01980000-0000-7000-8000-000000000008", logicalName: "arcade.zip",
		},
	} {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_metadata_revisions(
  id,game_id,title,description,developer,publisher,genre,source_kind,source_ref_id,created_at_ms
) VALUES(?,?,'Migration fixture','','','','','IMPORT_REVIEW','fixture',1000)
`, fixture.metadataID, fixture.gameID); err != nil {
			_ = transaction.Rollback()
			t.Fatal(err)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_content_revisions(
  id,game_id,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,content_kind,created_at_ms
) VALUES(?,?,'IMPORT_REVIEW',?,'[]','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','SINGLE_FILE',1000)
`, fixture.contentID, fixture.gameID, fixture.logicalName); err != nil {
			_ = transaction.Rollback()
			t.Fatal(err)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO games(
  id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,
  search_text,version,created_at_ms,updated_at_ms
) VALUES(?,?,'PUBLISHED',?,?,'migration fixture',1,1000,1000)
`, fixture.gameID, fixture.directoryID, fixture.metadataID, fixture.contentID); err != nil {
			_ = transaction.Rollback()
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}

	applyMigrationRange(ctx, t, database, repositoryRoot, 39, 39)

	moved := queryStrings(t, database, `
SELECT id||':'||platform_instance_id||':'||version FROM games ORDER BY id
`)
	wantMoved := []string{
		"01980000-0000-7000-8000-00000000a101:01980000-0000-7000-8000-000000000001:2",
		"01980000-0000-7000-8000-00000000a201:01980000-0000-7000-8000-000000000007:2",
	}
	testassert.Falsef(t, testassert.Any(func() bool { return len(moved) != len(wantMoved) }, func() bool { return moved[0] != wantMoved[0] }, func() bool { return moved[1] != wantMoved[1] }), "moved games = %#v, want %#v", moved, wantMoved)
	retired := queryStrings(t, database, `
SELECT id||':'||enabled||':'||(deleted_at_ms IS NOT NULL)
FROM platform_instances
WHERE id IN (
  '01980000-0000-7000-8000-000000000002',
  '01980000-0000-7000-8000-000000000008'
)
ORDER BY id
`)
	wantRetired := []string{
		"01980000-0000-7000-8000-000000000002:0:1",
		"01980000-0000-7000-8000-000000000008:0:1",
	}
	testassert.Falsef(t, testassert.Any(func() bool { return len(retired) != len(wantRetired) }, func() bool { return retired[0] != wantRetired[0] }, func() bool { return retired[1] != wantRetired[1] }), "retired directories = %#v, want %#v", retired, wantRetired)
	var metadataCount, contentCount int
	if err := database.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM game_metadata_revisions),(SELECT count(*) FROM game_content_revisions)
`).Scan(&metadataCount, &contentCount); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return metadataCount != 2 }, func() bool { return contentCount != 2 }), "historical revisions = metadata:%d content:%d", metadataCount, contentCount)
	var foreignKeyViolations int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_check`).Scan(&foreignKeyViolations); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, foreignKeyViolations != 0, "foreign key violations = %d", foreignKeyViolations)
}

func TestPlatformDirectoryConsolidationRevivesDeletedDestinations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "revive.db"))
	testassert.False(t, err != nil, err)
	defer func() { cleanup.Error("close", database.Close()) }()
	database.SetMaxOpenConns(1)
	if _, err := database.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, migrationTable); err != nil {
		t.Fatal(err)
	}
	applyMigrationRange(ctx, t, database, repositoryRoot, 1, 38)
	if _, err := database.ExecContext(ctx, `
UPDATE platform_instances SET enabled=0,version=2,updated_at_ms=1000,deleted_at_ms=1000
WHERE id IN (
  '01980000-0000-7000-8000-000000000001',
  '01980000-0000-7000-8000-000000000007'
)
`); err != nil {
		t.Fatal(err)
	}

	applyMigrationRange(ctx, t, database, repositoryRoot, 39, 39)
	active := queryStrings(t, database, `
SELECT id||':'||enabled||':'||(deleted_at_ms IS NULL)||':'||version
FROM platform_instances
WHERE id IN (
  '01980000-0000-7000-8000-000000000001',
  '01980000-0000-7000-8000-000000000007'
)
ORDER BY id
`)
	wantActive := []string{
		"01980000-0000-7000-8000-000000000001:1:1:3",
		"01980000-0000-7000-8000-000000000007:1:1:3",
	}
	testassert.Falsef(t, testassert.Any(func() bool { return len(active) != len(wantActive) }, func() bool { return active[0] != wantActive[0] }, func() bool { return active[1] != wantActive[1] }), "revived directories = %#v, want %#v", active, wantActive)
}
