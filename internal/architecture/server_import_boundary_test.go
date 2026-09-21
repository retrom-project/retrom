package architecture

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestServerImportHTTPUsesApplicationService(t *testing.T) {
	entries, err := os.ReadDir("../httpapi")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		name := filepath.Join("../httpapi", entry.Name())
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, dependency := range file.Imports {
			path, err := strconv.Unquote(dependency.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if path == "retrom/internal/serverimport" || path == "retrom/internal/pegasusimport" {
				t.Errorf("%s still depends on legacy server import package", name)
			}
		}
	}
}
