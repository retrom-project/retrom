package architecture

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/store"
)

func TestProductAndRuntimeSchemaDoNotReferenceWorkflowTables(t *testing.T) {
	t.Parallel()
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, table := range []string{
		"games", "game_files", "game_assets", "game_variants", "variant_files", "variant_dependencies",
		"launch_sessions", "launch_content_files", "launch_external_files", "save_states", "game_save_versions",
		"play_sessions", "runtime_preview_sessions", "runtime_preview_files",
		"isolated_runtime_bootstrap_tickets", "isolated_runtime_capabilities",
	} {
		t.Run(table, func(t *testing.T) {
			targets, err := dbapi.QueryStrings(t.Context(), database.SQL, `SELECT "table" FROM pragma_foreign_key_list(?)`, table)
			if err != nil {
				t.Fatal(err)
			}
			for _, target := range targets {
				if strings.HasPrefix(target, "import_") || strings.HasPrefix(target, "review_") || strings.HasPrefix(target, "source_import") {
					t.Errorf("product/runtime table %s depends on workflow table %s", table, target)
				}
			}
		})
	}
}
