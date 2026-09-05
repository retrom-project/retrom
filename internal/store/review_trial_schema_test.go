package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestFreshDatabaseHasNoRuntimeProofWorkflow(t *testing.T) {
	t.Parallel()
	database, err := Open(t.Context(), filepath.Join(t.TempDir(), "review.db"), func() time.Time {
		return time.UnixMilli(0)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	var count int
	if err := database.SQL.QueryRowContext(t.Context(), `
SELECT count(*) FROM sqlite_schema
WHERE name LIKE 'rpgmaker_runtime_validation%'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("fresh schema still contains %d production runtime-proof objects", count)
	}
}
