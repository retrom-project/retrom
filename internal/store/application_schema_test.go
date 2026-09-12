package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestApplicationQueriesDoNotRequireDatabaseViews(t *testing.T) {
	t.Parallel()
	database, err := Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), func() time.Time {
		return time.UnixMilli(1786000000000)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	views := queryStrings(t, database.SQL, "SELECT name FROM sqlite_schema WHERE type='view' ORDER BY name")
	if len(views) != 0 {
		t.Fatalf("application queries must own their projections; database views remain: %v", views)
	}
}

func TestApplicationSchemaHasNoTriggers(t *testing.T) {
	t.Parallel()
	database, err := Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	names := queryStrings(t, database.SQL, "SELECT name FROM sqlite_schema WHERE type='trigger' ORDER BY name")
	if len(names) > 0 {
		t.Fatalf("business writes must be explicit; %d database triggers remain", len(names))
	}
}
