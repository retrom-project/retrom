package store

import (
	"testing"
	"time"

	"retrom/internal/testsupport/testpostgres"

	dbapi "retrom/internal/database"
)

func TestSchemaHasNoNetplayState(t *testing.T) {
	t.Parallel()
	database, err := Open(t.Context(), testpostgres.DSN(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	var count int
	if err := dbapi.QueryRowContext(t.Context(), database.SQL, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname LIKE '%netplay%'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("netplay schema objects: count=%d error=%v", count, err)
	}
	if err := dbapi.QueryRowContext(t.Context(), database.SQL, `SELECT count(*) FROM (SELECT column_name AS name,data_type AS type FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='launch_sessions') WHERE name IN ('netplay_session_id','netplay_player_no','save_access')`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("netplay launch columns: count=%d error=%v", count, err)
	}
}
