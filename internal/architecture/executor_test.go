package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLExecutionInterfacesBelongToDatabase(t *testing.T) {
	t.Parallel()
	definitions := 0
	err := filepath.WalkDir("..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			contract, ok := node.(*ast.InterfaceType)
			if !ok || !sqlExecutionMethods(contract) {
				return true
			}
			definitions++
			validateSQLInterface(t, path, contract)
			return false
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if definitions == 0 {
		t.Fatal("database SQL execution interfaces are missing")
	}
}

func validateSQLInterface(t *testing.T, path string, contract *ast.InterfaceType) {
	t.Helper()
	if !strings.HasPrefix(path, filepath.Join("..", "database")+string(filepath.Separator)) {
		t.Errorf("%s declares SQL execution; reuse an internal/database interface", path)
	}
	for _, method := range contract.Methods.List {
		for _, name := range method.Names {
			if name.Name == "QueryRowContext" {
				t.Errorf("%s exposes QueryRowContext as a method; use database.QueryRowContext", path)
			}
		}
	}
}

func sqlExecutionMethods(contract *ast.InterfaceType) bool {
	names := map[string]bool{}
	for _, method := range contract.Methods.List {
		for _, name := range method.Names {
			names[name.Name] = true
		}
	}
	return names["ExecContext"] || names["QueryContext"] || names["QueryRowContext"]
}
