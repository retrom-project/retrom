package maintenance

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"retrom/internal/testkit/testsupport"
)

func TestRestoredScanCleanupRefusesExistingTagOwnership(t *testing.T) {
	t.Parallel()
	db := restoredScanDatabase(t, true)
	for _, query := range []string{
		`INSERT INTO tags(id,name,name_key,search_text,status,created_by_user_id,updated_by_user_id,created_at_ms,updated_at_ms)VALUES('019b0000-0000-7000-8000-000000000001','Tag','tag','tag','ACTIVE','actor','actor',1,1)`,
		`INSERT INTO emulationstation_collection_tags(collection_id,tag_id,assigned_by_user_id,created_at_ms)VALUES('collection','019b0000-0000-7000-8000-000000000001','actor',1)`,
	} {
		if _, err := db.ExecContext(t.Context(), query); err != nil {
			t.Fatal(err)
		}
	}
	before := restoredScanSnapshot(t, db)
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fenceRestoredEmulationStation(t.Context(), tx, 2000); err == nil {
		t.Fatal("restore deleted owned scan projection")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if before != restoredScanSnapshot(t, db) {
		t.Fatal("failed ownership check changed restored data")
	}
}

func TestRestoredScanFailuresRollbackJobProjectionAndCounters(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"job", "assets", "items", "counters", "aggregate"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			db := restoredScanDatabase(t, true)
			before := restoredScanSnapshot(t, db)
			failure := errors.New("restore storage failed")
			var hits atomic.Int64
			faultDB := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
				if matchesRestoreFault(stage, query, args) {
					hits.Add(1)
					return failure
				}
				return nil
			}})
			tx, err := faultDB.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = fenceRestoredEmulationStation(t.Context(), tx, 2000)
			if !errors.Is(err, failure) || hits.Load() != 1 {
				t.Fatalf("restore=%v hits=%d", err, hits.Load())
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			if before != restoredScanSnapshot(t, db) {
				t.Fatal("restore failure retained writes")
			}
		})
	}
}

func matchesRestoreFault(stage, query string, args []driver.NamedValue) bool {
	normalized := strings.Join(strings.Fields(query), " ")
	prefixes := map[string]string{"job": "UPDATE jobs SET state='FAILED'", "assets": "DELETE FROM emulationstation_import_item_assets", "items": "DELETE FROM emulationstation_import_items", "counters": "UPDATE emulationstation_imports SET gamelist_count=0", "aggregate": "UPDATE emulationstation_imports SET state='FAILED'"}
	if !strings.HasPrefix(normalized, prefixes[stage]) {
		return false
	}
	for _, arg := range args {
		if arg.Value == "scan" {
			return true
		}
		if (stage == "job" || stage == "aggregate") && arg.Value == int64(2000) {
			return true
		}
	}
	return false
}

func restoredScanSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	snapshot := map[string][][]any{}
	for _, table := range []string{"jobs", "emulationstation_imports", "emulationstation_import_gamelists", "emulationstation_import_collections", "emulationstation_collection_tags", "emulationstation_import_items", "emulationstation_import_item_files", "emulationstation_import_item_assets"} {
		snapshot[table] = restoredTableRows(t, db, table)
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func restoredTableRows(t *testing.T, db *sql.DB, table string) [][]any {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "SELECT * FROM "+table+" ORDER BY rowid")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Error(err)
		}
	}()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	result := [][]any{}
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		result = append(result, values)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}
