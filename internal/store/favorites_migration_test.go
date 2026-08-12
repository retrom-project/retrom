package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
)

const (
	migrationProfileA = "01980000-0000-7000-8000-00000000a201"
	migrationProfileB = "01980000-0000-7000-8000-00000000a202"
	migrationGame     = "01980000-0000-7000-8000-00000000f201"
	migrationFolderA  = "01980000-0000-7000-8000-00000000c201"
	migrationFolderB  = "01980000-0000-7000-8000-00000000c202"
)

func seedFavoriteConstraintRows(t *testing.T, database *sql.DB) {
	t.Helper()
	if _, err := database.Exec(`
PRAGMA defer_foreign_keys=ON;
BEGIN;
INSERT INTO profiles(id,display_name,created_at_ms) VALUES
  ('` + migrationProfileA + `','Favorite A',1000),
  ('` + migrationProfileB + `','Favorite B',1000);
INSERT INTO game_metadata_revisions(
  id,game_id,title,description,developer,publisher,genre,players,release_year,
  source_kind,source_ref_id,created_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f202','` + migrationGame + `','Favorite Fixture',
  '','','','',NULL,2001,'ADMIN_EDIT',NULL,1000
);
INSERT INTO game_content_revisions(
  id,game_id,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f203','` + migrationGame + `','ADMIN_REPLACE',
  'favorite-fixture','[]','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',1000
);
INSERT INTO games(
  id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,
  search_text,version,created_at_ms,updated_at_ms
) VALUES(
  '` + migrationGame + `','01980000-0000-7000-8000-000000000005','PUBLISHED',
  '01980000-0000-7000-8000-00000000f202','01980000-0000-7000-8000-00000000f203',
  'favorite fixture',1,1000,1000
);
COMMIT;
`); err != nil {
		t.Fatal(err)
	}
}

func requireSQLFailure(t *testing.T, database *sql.DB, statement string, arguments ...any) {
	t.Helper()
	if _, err := database.Exec(statement, arguments...); err == nil {
		t.Fatalf("expected SQL failure: %s", statement)
	}
}

func TestFavoritesMigrationConstraintsAndIndexes(t *testing.T) {
	t.Parallel()
	database, err := Open(context.Background(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", database.Close()) }()
	seedFavoriteConstraintRows(t, database.SQL)
	for _, insert := range []struct {
		query string
		args  []any
	}{
		{
			`INSERT INTO favorite_games(profile_id,game_id,created_at_ms) VALUES(?,?,1000)`,
			[]any{migrationProfileA, migrationGame},
		},
		{
			`INSERT INTO favorite_folders(id,profile_id,name,name_key,version,created_at_ms,updated_at_ms)
VALUES(?,?,'Arcade','arcade',1,1000,1000),(?,?,'Arcade','arcade',1,1000,1000)`,
			[]any{migrationFolderA, migrationProfileA, migrationFolderB, migrationProfileB},
		},
		{
			`INSERT INTO favorite_folder_games(profile_id,folder_id,game_id,created_at_ms) VALUES(?,?,?,1000)`,
			[]any{migrationProfileA, migrationFolderA, migrationGame},
		},
	} {
		if _, err := database.SQL.Exec(insert.query, insert.args...); err != nil {
			t.Fatal(err)
		}
	}
	requireSQLFailure(t, database.SQL,
		`INSERT INTO favorite_folder_games(profile_id,folder_id,game_id,created_at_ms) VALUES(?,?,?,1000)`,
		migrationProfileA, migrationFolderB, migrationGame)
	requireSQLFailure(t, database.SQL,
		`INSERT INTO favorite_folder_games(profile_id,folder_id,game_id,created_at_ms) VALUES(?,?,?,1000)`,
		migrationProfileB, migrationFolderB, migrationGame)
	requireSQLFailure(t, database.SQL,
		`INSERT INTO favorite_folders(id,profile_id,name,name_key,version,created_at_ms,updated_at_ms) VALUES(?,?,'ARCADE','arcade',1,1000,1000)`,
		"01980000-0000-7000-8000-00000000c203", migrationProfileA)
	requireSQLFailure(t, database.SQL,
		`INSERT INTO favorite_folders(id,profile_id,name,name_key,version,created_at_ms,updated_at_ms) VALUES('zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz',?,'Bad','bad',1,1000,1000)`,
		migrationProfileA)
	requireSQLFailure(t, database.SQL,
		`UPDATE favorite_games SET created_at_ms=2000 WHERE profile_id=? AND game_id=?`, migrationProfileA, migrationGame)
	requireSQLFailure(t, database.SQL,
		`UPDATE favorite_folder_games SET created_at_ms=2000 WHERE profile_id=? AND folder_id=? AND game_id=?`,
		migrationProfileA, migrationFolderA, migrationGame)
	requireSQLFailure(t, database.SQL,
		`UPDATE favorite_folders SET name='Changed',name_key='changed',version=3,updated_at_ms=2000 WHERE id=?`,
		migrationFolderA)
	requireSQLFailure(t, database.SQL,
		`DELETE FROM favorite_games WHERE profile_id=? AND game_id=?`, migrationProfileA, migrationGame)
	if _, err := database.SQL.Exec(`
UPDATE favorite_folders SET name='Changed',name_key='changed',version=2,updated_at_ms=2000 WHERE id=?
`, migrationFolderA); err != nil {
		t.Fatalf("valid folder rename: %v", err)
	}
	planRows, err := database.SQL.Query(`
EXPLAIN QUERY PLAN SELECT game_id FROM favorite_games
WHERE profile_id=? ORDER BY created_at_ms DESC,game_id DESC
`, migrationProfileA)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", planRows.Close()) }()
	var plan strings.Builder
	for planRows.Next() {
		var id, parent, unused int
		var detail string
		if err := planRows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail)
	}
	if err := planRows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "favorite_games_profile_created") {
		t.Fatalf("query plan = %s", plan.String())
	}
	if err := database.IntegrityCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFavoritesMigrationUpgradesVersion24AndPreservesFixture(t *testing.T) {
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
	applyMigrationRange(ctx, t, legacy, repositoryRoot, 1, 24)
	fixture, err := os.ReadFile(filepath.Join(repositoryRoot, "migrations", "testdata", "024_fixture.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, string(fixture)); err != nil {
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
	var version, userCount, idempotencyCount int
	if err := upgraded.SQL.QueryRow(`
SELECT (SELECT max(version) FROM schema_migrations),
       (SELECT count(*) FROM users WHERE username LIKE 'migration024.%'),
       (SELECT count(*) FROM idempotency_records WHERE operation_id='fixture-024-operation')
`).Scan(&version, &userCount, &idempotencyCount); err != nil {
		t.Fatal(err)
	}
	if version != 29 || userCount != 2 || idempotencyCount != 1 {
		t.Fatalf("upgrade values = version:%d users:%d idempotency:%d", version, userCount, idempotencyCount)
	}
}
