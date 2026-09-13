package repo

import (
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestSQLInfrastructureLivesInRepo(t *testing.T) {
	t.Parallel()
	repoRoot := architecture.PackageDirectory(t)
	internalRoot := filepath.Dir(repoRoot)
	for _, name := range []string{"recordstore", "sessionstore", "storequery", "blobregistry"} {
		if _, err := os.Stat(filepath.Join(internalRoot, name)); !os.IsNotExist(err) {
			t.Errorf("SQL infrastructure %s must live under repo", name)
		}
		if _, err := os.Stat(filepath.Join(repoRoot, name)); err != nil {
			t.Errorf("missing repo/%s: %v", name, err)
		}
	}
}
