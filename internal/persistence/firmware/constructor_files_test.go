package firmware

import (
	"testing"

	"retrom/internal/filestore"
)

func constructorFiles(t *testing.T) *filestore.Store {
	t.Helper()
	files, err := filestore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return files
}
