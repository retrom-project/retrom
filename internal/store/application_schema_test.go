package store

import (
	"testing"
	"time"

	"retrom/internal/testsupport/testpostgres"
)

func TestApplicationQueriesDoNotRequireDatabaseViews(t *testing.T) {
	t.Parallel()
	database, err := Open(t.Context(), testpostgres.DSN(t), func() time.Time {
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
	views := queryStrings(t, database.SQL, "SELECT name FROM (SELECT c.relname AS name, CASE c.relkind WHEN 'r' THEN 'table' WHEN 'i' THEN 'index' WHEN 'v' THEN 'view' END AS type FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()) objects WHERE type='view' ORDER BY name")
	if len(views) != 0 {
		t.Fatalf("application queries must own their projections; database views remain: %v", views)
	}
}

func TestApplicationSchemaHasNoTriggers(t *testing.T) {
	t.Parallel()
	database, err := Open(t.Context(), testpostgres.DSN(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	names := queryStrings(t, database.SQL, "SELECT name FROM (SELECT c.relname AS name, CASE c.relkind WHEN 'r' THEN 'table' WHEN 'i' THEN 'index' WHEN 'v' THEN 'view' END AS type FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()) objects WHERE type='trigger' ORDER BY name")
	if len(names) != 0 {
		t.Fatalf("unexpected database triggers: %v", names)
	}
}
