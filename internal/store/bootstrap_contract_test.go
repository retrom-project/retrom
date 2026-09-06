package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"retrom/internal/runtimecatalog"
)

func TestBootstrapCreatesFinalSchemaWithoutLegacyConversion(t *testing.T) {
	t.Parallel()
	sources, err := migrationSources()
	if err != nil {
		t.Fatal(err)
	}
	forbidden := regexp.MustCompile(`(?im)\b(DROP|ALTER)\s+(TABLE|TRIGGER|VIEW|INDEX)\b|__new_|revision_no|retrom:foreign-keys-off|game_(content|metadata|variant)_revisions|INSERT\s+INTO\s+(platforms|cores|platform_cores|content_kinds|runtime_asset_pack_definitions)\b`)
	// Compatible extensions may replace triggers, but never convert or drop payload tables.
	extensionForbidden := regexp.MustCompile(`(?im)\b(DROP|ALTER)\s+(TABLE|VIEW|INDEX)\b|__new_|revision_no|retrom:foreign-keys-off|game_(content|metadata|variant)_revisions`)
	for _, source := range sources {
		if match := extensionForbidden.Find(source.contents); match != nil {
			t.Errorf("migration %s contains destructive conversion %q", source.name, match)
		}
	}
	// 001–010 are the frozen clean bootstrap. Later migrations upgrade existing databases.
	for _, source := range sources[:10] {
		if match := forbidden.Find(source.contents); match != nil {
			t.Errorf("bootstrap %s contains legacy conversion %q", source.name, match)
		}
	}
}

func seedSchemaProductDefinitions(t *testing.T, database *sql.DB) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "data", "runtime-target-bindings", "v1", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := runtimecatalog.ParseCatalog(contents)
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback() }()
	if err := runtimecatalog.SynchronizeDefinitions(t.Context(), transaction, catalog, 0); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
}
