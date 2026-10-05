package store

import (
	"testing"
	"time"

	"retrom/internal/testsupport/testpostgres"

	dbapi "retrom/internal/database"
)

func TestFreshDatabaseHasNoRuntimeProofWorkflow(t *testing.T) {
	t.Parallel()
	database, err := Open(t.Context(), testpostgres.DSN(t), func() time.Time {
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
	if err := dbapi.QueryRowContext(t.Context(), database.SQL, `
SELECT count(*) FROM (`+testpostgres.SchemaObjectsSQL+`) objects
WHERE name LIKE 'rpgmaker_runtime_validation%'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("fresh schema still contains %d production runtime-proof objects", count)
	}
}
