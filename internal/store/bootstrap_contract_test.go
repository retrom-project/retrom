package store

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	dbapi "retrom/internal/database"

	runtimecatalogpersistence "retrom/internal/persistence/runtimecatalog"

	runtimecatalog "retrom/internal/runtime/catalog"
)

func TestBootstrapCreatesFinalSchemaWithoutLegacyConversion(t *testing.T) {
	t.Parallel()
	sources, err := migrationSources()
	if err != nil {
		t.Fatal(err)
	}
	forbidden := regexp.MustCompile(`(?im)\bDROP\s+(TABLE|TRIGGER|VIEW|INDEX)\b|CREATE\s+VIEW\b|__new_|revision_no|retrom:foreign-keys-off|game_(content|metadata|variant)_revisions|INSERT\s+INTO\s+(platforms|cores|platform_cores|content_kinds|game_save_versions|launch_game_save_bindings|launch_payload_retirements)\b`)
	for _, source := range sources {
		for _, statement := range strings.Split(string(source.contents), ";") {
			if strings.HasPrefix(strings.TrimSpace(statement), "ALTER TABLE") && !strings.Contains(statement, " ADD FOREIGN KEY") {
				t.Fatalf("bootstrap changes existing columns: %s", statement)
			}
		}
		if match := forbidden.Find(source.contents); match != nil {
			t.Errorf("bootstrap %s contains legacy conversion or implicit business logic %q", source.name, match)
		}
	}
}

func seedSchemaProductDefinitions(t *testing.T, database dbapi.DB) {
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
	if err := runtimecatalogpersistence.SynchronizeDefinitions(t.Context(), transaction, catalog, 0); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
}
