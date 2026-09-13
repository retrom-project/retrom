package architecture

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSQLInfrastructureLivesInPersistence(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"recordstore", "sessionstore", "storequery", "blobregistry"} {
		if _, err := os.Stat(filepath.Join("..", "..", name)); !os.IsNotExist(err) {
			t.Errorf("SQL infrastructure %s must live under persistence", name)
		}
		if _, err := os.Stat(filepath.Join("..", "..", "persistence", name)); err != nil {
			t.Errorf("missing persistence/%s: %v", name, err)
		}
	}
	assertBusinessImports(t, "../../adapter/files/blobstore")
	assertBusinessImports(t, "../../foundation/cleanup")
}
