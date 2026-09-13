package persistence

import (
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestSQLInfrastructureLivesInPersistence(t *testing.T) {
	t.Parallel()
	persistenceRoot := architecture.PackageDirectory(t)
	internalRoot := filepath.Dir(persistenceRoot)
	for _, name := range []string{"recordstore", "sessionstore", "storequery", "blobregistry"} {
		if _, err := os.Stat(filepath.Join(internalRoot, name)); !os.IsNotExist(err) {
			t.Errorf("SQL infrastructure %s must live under persistence", name)
		}
		if _, err := os.Stat(filepath.Join(persistenceRoot, name)); err != nil {
			t.Errorf("missing persistence/%s: %v", name, err)
		}
	}
}
