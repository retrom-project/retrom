package httpapi

import (
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestServerImportHTTPUsesApplicationService(t *testing.T) {
	entries, err := architecture.GoSourceFiles(t)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range entries {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, dependency := range file.Imports {
			path, err := strconv.Unquote(dependency.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if path == "retrom/internal/serverimport" || path == "retrom/internal/adapter/imports/pegasusimport" {
				t.Errorf("%s still depends on legacy server import package", name)
			}
		}
	}
}
