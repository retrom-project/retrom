//go:build integration

package refactor

import (
	"database/sql"
	"testing"
)

func readCharacterizationSchema(t *testing.T, database *sql.DB) []schemaObject {
	t.Helper()
	rows, err := database.QueryContext(t.Context(),
		"SELECT type,name,tbl_name,sql FROM sqlite_schema ORDER BY type,name,tbl_name")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Error(err)
		}
	}()
	result := make([]schemaObject, 0)
	for rows.Next() {
		var entry schemaObject
		if err := rows.Scan(&entry.Kind, &entry.Name, &entry.Table, &entry.SQL); err != nil {
			t.Fatal(err)
		}
		result = append(result, entry)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func readCharacterizationLineage(t *testing.T, database *sql.DB) []lineageEntry {
	t.Helper()
	rows, err := database.QueryContext(t.Context(),
		"SELECT version,name,checksum,applied_at_ms FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Error(err)
		}
	}()
	result := make([]lineageEntry, 0)
	for rows.Next() {
		var entry lineageEntry
		if err := rows.Scan(&entry.Version, &entry.Name, &entry.Checksum, &entry.AppliedAtMS); err != nil {
			t.Fatal(err)
		}
		result = append(result, entry)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}
