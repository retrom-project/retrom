package sourceimport

import (
	"database/sql"
	"testing"
	"time"

	tagrepository "retrom/internal/persistence/tagging"
	"retrom/internal/service/tagging"
)

func TestItemsRejectCorruptStoredWarnings(t *testing.T) {
	t.Parallel()
	for _, stored := range []string{"{", "{}"} {
		t.Run(stored, func(t *testing.T) {
			t.Parallel()
			db := newSourceRetryDatabase(t)
			seedQueryCollection(t, db)
			if _, err := db.ExecContext(t.Context(), `UPDATE source_import_items SET warnings_json=? WHERE id='item'`, stored); err != nil {
				t.Fatal(err)
			}
			service := &Service{database: db}
			items, err := service.Items(t.Context(), "import", "", "", "", "", "", "", 10)
			if err == nil || items != nil {
				t.Fatalf("corrupt warnings returned successful items: %#v, error=%v", items, err)
			}
		})
	}
}

func seedQueryCollection(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), `INSERT INTO source_import_collections(id,import_id,metadata_relative_path,segment_ordinal,name,game_count,created_at_ms,updated_at_ms) VALUES('collection','import','metadata.pegasus.txt',0,'Collection',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE source_import_items SET collection_id='collection' WHERE id='item'`); err != nil {
		t.Fatal(err)
	}
}

func TestItemsWithoutCollectionHaveEmptyTags(t *testing.T) {
	t.Parallel()
	service := &Service{database: newSourceRetryDatabase(t)}
	items, err := service.Items(t.Context(), "import", "", "", "", "", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Tags == nil || len(items[0].Tags) != 0 {
		t.Fatalf("uncollected item: %#v", items)
	}
}

func TestItemsNormalizeAbsentWarningsToEmptyArray(t *testing.T) {
	t.Parallel()
	db := newSourceRetryDatabase(t)
	seedQueryCollection(t, db)
	if _, err := db.ExecContext(t.Context(), `UPDATE source_import_items SET warnings_json='null' WHERE id='item'`); err != nil {
		t.Fatal(err)
	}
	values, err := (&Service{database: db}).Items(t.Context(), "import", "", "", "", "", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].Warnings == nil || len(values[0].Warnings) != 0 {
		t.Fatalf("absent warnings: %#v", values)
	}
}

func TestItemsRejectInvalidStructuredDiagnostics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, statement string }{
		{"failure_details", `UPDATE source_import_items SET error_details_json='{"stage":7}' WHERE id='item'`},
		{"existing_matches", `UPDATE source_import_items SET existing_matches_json='[{"gameId":7}]' WHERE id='item'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := newSourceRetryDatabase(t)
			seedQueryCollection(t, db)
			if _, err := db.ExecContext(t.Context(), tc.statement); err != nil {
				t.Fatal(err)
			}
			values, err := (&Service{database: db}).Items(t.Context(), "import", "", "", "", "", "", "", 10)
			if err == nil || values != nil {
				t.Fatalf("invalid structured diagnostic: %#v, %v", values, err)
			}
		})
	}
}

func TestCollectionsRejectCorruptStoredRules(t *testing.T) {
	t.Parallel()
	for _, statement := range []string{
		`UPDATE source_import_collections SET ignored_rules_json='{' WHERE id='collection'`,
		`UPDATE source_import_collections SET warning_fields_json='{}' WHERE id='collection'`,
	} {
		t.Run(statement, func(t *testing.T) {
			t.Parallel()
			db := newSourceRetryDatabase(t)
			seedQueryCollection(t, db)
			if _, err := db.ExecContext(t.Context(), statement); err != nil {
				t.Fatal(err)
			}
			service := &Service{database: db, tags: tagging.New(tagrepository.New(db), time.Now)}
			values, err := service.Collections(t.Context(), "import", "", 0, "", 10)
			if err == nil || values != nil {
				t.Fatalf("corrupt rules returned successful collections: %#v, %v", values, err)
			}
		})
	}
}
