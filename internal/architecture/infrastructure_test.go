package architecture

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSQLInfrastructureLivesInPersistence(t *testing.T) {
	t.Parallel()
	root := sourceRoot(t)
	for _, name := range []string{"recordstore", "sessionstore", "storequery", "filecatalog", "fileownership"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("SQL infrastructure %s must live under persistence", name)
		}
		if _, err := os.Stat(filepath.Join(root, "persistence", name)); err != nil {
			t.Errorf("missing persistence/%s: %v", name, err)
		}
	}
}
