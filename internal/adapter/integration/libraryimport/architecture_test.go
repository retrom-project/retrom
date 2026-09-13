package libraryimport

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestLibraryImportFacadeDoesNotOwnSQLExecution(t *testing.T) {
	entries, err := architecture.GoSourceFiles(t)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range entries {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case "BeginTx", "ExecContext", "QueryContext", "QueryRowContext":
				t.Errorf("%s calls %s directly; move SQL execution to persistence", name, selector.Sel.Name)
			}
			return true
		})
	}
}
