// Command sql-boundary rejects generic persistence SQL access from domain services.
package main

import (
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"strings"

	"golang.org/x/tools/go/packages"
)

func main() {
	findings, err := check("", "./internal/service/...")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	for _, finding := range findings {
		fmt.Fprintln(os.Stderr, finding)
	}
	if len(findings) != 0 {
		os.Exit(1)
	}
	fmt.Println("SQL service boundary: PASS")
}

func check(directory string, patterns ...string) ([]string, error) {
	loaded, err := packages.Load(&packages.Config{
		Dir: directory,
		Mode: packages.NeedName | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedImports | packages.NeedDeps,
	}, patterns...)
	if err != nil {
		return nil, fmt.Errorf("load service packages: %w", err)
	}
	var findings []string
	for _, pkg := range loaded {
		if len(pkg.Errors) != 0 {
			return nil, fmt.Errorf("type-check %s: %s", pkg.PkgPath, pkg.Errors[0])
		}
		if !strings.HasPrefix(pkg.PkgPath, "retrom/internal/service/") {
			continue
		}
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				selection := pkg.TypesInfo.Selections[selector]
				if selection != nil && genericSQLMethod(selection.Obj()) {
					findings = append(findings, fmt.Sprintf("%s: services must call named persistence operations, not %s",
						pkg.Fset.Position(selector.Pos()), selection.Obj().Name()))
				}
				return true
			})
		}
	}
	return findings, nil
}

func genericSQLMethod(object types.Object) bool {
	method, ok := object.(*types.Func)
	if !ok || (method.Name() != "Count" && method.Name() != "Execute") {
		return false
	}
	signature, ok := method.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return false
	}
	receiver := signature.Recv().Type()
	if pointer, isPointer := receiver.(*types.Pointer); isPointer {
		receiver = pointer.Elem()
	}
	named, ok := types.Unalias(receiver).(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "retrom/internal/persistence" &&
		named.Obj().Name() == "Repository"
}
