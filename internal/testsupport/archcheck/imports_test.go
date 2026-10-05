package archcheck

import "testing"

func TestBusinessImportBoundaries(t *testing.T) {
	for _, dependency := range []string{
		"database/sql", "retrom/internal/persistence/libraryimport", "retrom/internal/database/postgres",
		"retrom/internal/application", "retrom/internal/composition/cleanupjobs",
		"retrom/internal/httpapi", "retrom/cmd/retrom",
	} {
		if !forbiddenBusinessImport(dependency) {
			t.Errorf("accepted forbidden dependency %s", dependency)
		}
	}
	for _, dependency := range []string{
		"context", "retrom/internal/service/libraryimport", "retrom/internal/filestore",
		"retrom/internal/applicationfacts", "retrom/internal/compositionfacts",
	} {
		if forbiddenBusinessImport(dependency) {
			t.Errorf("rejected legal dependency %s", dependency)
		}
	}
}
