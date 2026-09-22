package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSchemaHasNoNetplayState(t *testing.T) {
	t.Parallel()
	database, err := Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	var count int
	if err := database.SQL.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_schema WHERE name LIKE '%netplay%'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("netplay schema objects: count=%d error=%v", count, err)
	}
	if err := database.SQL.QueryRowContext(t.Context(), `SELECT count(*) FROM pragma_table_info('launch_sessions') WHERE name IN ('netplay_session_id','netplay_player_no','save_access')`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("netplay launch columns: count=%d error=%v", count, err)
	}
}
