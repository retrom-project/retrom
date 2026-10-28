package migrations

import (
	"testing"
	"testing/fstest"
)

func TestMigrationsIncludeEveryNumberedFileInOrder(t *testing.T) {
	items, err := readMigrations(fstest.MapFS{
		"002_indexes.sql": {Data: []byte("SELECT 2;")},
		"001_schema.sql":  {Data: []byte("SELECT 1;")},
	})
	if err != nil || len(items) != 2 || items[0].ID != "001_schema" || items[1].SQL != "SELECT 2;" {
		t.Fatalf("migrations=%v err=%v", items, err)
	}
	actual, err := All()
	if err != nil || len(actual) == 0 || actual[0].ID != "001_schema" {
		t.Fatalf("embedded migrations=%v err=%v", actual, err)
	}
}

func TestMigrationsRejectMissingDuplicateAndInvalidNumbers(t *testing.T) {
	for name, files := range map[string]fstest.MapFS{
		"empty":     {},
		"gap":       {"001_schema.sql": {Data: []byte("SELECT 1;")}, "003_next.sql": {Data: []byte("SELECT 3;")}},
		"duplicate": {"001_a.sql": {Data: []byte("SELECT 1;")}, "001_b.sql": {Data: []byte("SELECT 1;")}},
		"invalid":   {"schema.sql": {Data: []byte("SELECT 1;")}},
		"blank":     {"001_schema.sql": {Data: []byte(" \n")}},
	} {
		t.Run(name, func(t *testing.T) {
			if items, err := readMigrations(files); err == nil {
				t.Fatalf("accepted %v", items)
			}
		})
	}
}
