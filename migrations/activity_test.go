package migrations_test

import (
	"strings"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/store"
	"retrom/internal/testsupport/testpostgres"
)

func TestFreshSchemaCreatesEmptyActivityProjectionAndPageIndexes(t *testing.T) {
	opened, err := store.Open(t.Context(), testpostgres.DSN(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	db := opened.SQL
	if _, err := db.ExecContext(t.Context(), "SET enable_seqscan=off; SET enable_bitmapscan=off"); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := dbapi.QueryRowContext(t.Context(), db, "SELECT count(*) FROM profile_game_activity").Scan(&count); err != nil || count != 0 {
		t.Fatalf("fresh activity count=%d error=%v", count, err)
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
	rows, err := db.QueryContext(t.Context(), "EXPLAIN "+query)
	if err != nil {
		t.Fatal(err)
	}
	var details []string
	for rows.Next() {
		var detail string
		if err := rows.Scan(&detail); err != nil {
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
	if !strings.Contains(plan, index) || strings.Contains(plan, "Sort ") || !strings.Contains(plan, "Index") {
		t.Fatalf("unbounded page plan for %s: %s", index, plan)
	}
}
