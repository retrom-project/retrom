package dbexec

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"retrom/internal/testkit/architecture"
)

func TestSQLExecutorHasOneDefinition(t *testing.T) {
	t.Parallel()
	definitions := 0
	repoRoot := filepath.Dir(architecture.PackageDirectory(t))
	err := filepath.WalkDir(repoRoot, func(path string, entry fs.DirEntry, err error) error {
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
			if path != filepath.Join(repoRoot, "dbexec", "executor.go") {
				t.Errorf("%s redeclares SQL execution; reuse dbexec.Executor", path)
			}
			return false
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if definitions != 1 {
		t.Fatalf("SQL execution interface definitions = %d, want 1", definitions)
	}
}

func sqlExecutionMethods(contract *ast.InterfaceType) bool {
	names := map[string]bool{}
	for _, method := range contract.Methods.List {
		for _, name := range method.Names {
			names[name.Name] = true
		}
	}
	return names["ExecContext"] && names["QueryContext"] && names["QueryRowContext"]
}
