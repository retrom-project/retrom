package architecture

import (
	"io/fs"
	"path/filepath"
	"testing"

	"retrom/internal/testsupport/archcheck"
)

func TestServicesDependOnBusinessPorts(t *testing.T) {
	t.Parallel()
	err := filepath.WalkDir(filepath.Join(sourceRoot(t), "service"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			archcheck.AssertBusinessImportsIn(t, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
