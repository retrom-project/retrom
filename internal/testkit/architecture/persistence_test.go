package architecture

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestServicesDependOnBusinessPorts(t *testing.T) {
	t.Parallel()
	err := filepath.WalkDir("../../service", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			assertBusinessImports(t, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertBusinessImports(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		name := filepath.Join(directory, entry.Name())
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, dependency := range file.Imports {
			value, err := strconv.Unquote(dependency.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if databaseDependency(value) {
				t.Errorf("%s imports database implementation %s; depend on a business port", name, value)
			}
		}
	}
}

func databaseDependency(value string) bool {
	for _, prefix := range []string{
		"database/sql", "modernc.org/sqlite", "retrom/internal/persistence", "retrom/internal/persistence/dbexec",
		"retrom/internal/persistence/recordstore", "retrom/internal/persistence/sessionstore", "retrom/internal/persistence/storequery", "retrom/internal/persistence/store",
	} {
		if value == prefix || strings.HasPrefix(value, prefix+"/") {
			return true
		}
	}
	return false
}
