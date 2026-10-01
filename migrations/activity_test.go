package migrations_test

import (
	"strings"
	"testing"

	dbapi "retrom/internal/database"
	"retrom/internal/database/sqlite"
	"retrom/migrations"
)

func TestActivityProjectionInitializesCurrentLedgerAndUsesPageIndexes(t *testing.T) {
	db, err := sqlite.Open(":memory:", sqlite.Options{MaxOpenConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.ExecContext(t.Context(), `CREATE TABLE profiles(id TEXT PRIMARY KEY);
CREATE TABLE games(id TEXT PRIMARY KEY,status TEXT,title TEXT,created_at_ms INTEGER,updated_at_ms INTEGER);
CREATE TABLE game_assets(id TEXT PRIMARY KEY,game_id TEXT,kind TEXT,ordinal INTEGER);
CREATE TABLE play_sessions(id TEXT PRIMARY KEY,profile_id TEXT,game_id TEXT,started_at_ms INTEGER,active_duration_ms INTEGER);
INSERT INTO profiles VALUES ('a'),('b'); INSERT INTO games VALUES ('g','PUBLISHED','Game',1,2);
INSERT INTO play_sessions VALUES ('1','a','g',10,100),('2','a','g',20,200),('3','b','g',30,999);`)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := migrations.Files.ReadFile("016_library_query_indexes.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), string(contents)); err != nil {
		t.Fatal(err)
	}
	var last, duration, count int64
	if err := dbapi.QueryRowContext(t.Context(), db, `SELECT last_played_at_ms,active_duration_ms,session_count
FROM profile_game_activity WHERE profile_id='a' AND game_id='g'`).Scan(&last, &duration, &count); err != nil || last != 20 || duration != 300 || count != 2 {
		t.Fatalf("current projection: last=%d duration=%d count=%d err=%v", last, duration, count, err)
	}
	queries := []struct{ sql, index string }{
		{`SELECT game_id FROM profile_game_activity WHERE profile_id='a'
AND (last_played_at_ms,game_id)<(20,'z') ORDER BY last_played_at_ms DESC,game_id DESC LIMIT 50`, "profile_game_activity_recent"},
		{`SELECT game_id FROM profile_game_activity WHERE profile_id='a'
ORDER BY active_duration_ms DESC,last_played_at_ms DESC,game_id DESC LIMIT 50`, "profile_game_activity_duration"},
		{`SELECT game_id FROM profile_game_activity WHERE profile_id='a'
ORDER BY session_count DESC,last_played_at_ms DESC,game_id DESC LIMIT 50`, "profile_game_activity_sessions"},
		{`SELECT id FROM games WHERE updated_at_ms<=2 AND (updated_at_ms<2 OR
(updated_at_ms=2 AND (title>'A' OR (title='A' AND id>'a')))) ORDER BY updated_at_ms DESC,title,id LIMIT 6`, "games_updated"},
	}
	for _, query := range queries {
		assertActivityQueryPlan(t, db, query.sql, query.index)
	}
}

func assertActivityQueryPlan(t *testing.T, db dbapi.DB, query, index string) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+query)
	if err != nil {
		t.Fatal(err)
	}
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	rowErr := rows.Err()
	_ = rows.Close()
	if rowErr != nil {
		t.Fatal(rowErr)
	}
	plan := strings.Join(details, "\n")
	if !strings.Contains(plan, index) || strings.Contains(plan, "TEMP B-TREE") || !strings.Contains(plan, "SEARCH") {
		t.Fatalf("unbounded page plan for %s: %s", index, plan)
	}
}
