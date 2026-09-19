package architecture

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
)

// GoSourceInventory includes syntax even when build constraints exclude a file.
type GoSourceInventory struct {
	File         string   `json:"file"`
	Package      string   `json:"package"`
	Imports      []string `json:"imports"`
	Declarations []string `json:"declarations"`
}

// InspectGoSyntax covers every tracked or nonignored Go source, regardless of tags.
func InspectGoSyntax(root string, sources []string) ([]GoSourceInventory, error) {
	result := make([]GoSourceInventory, 0)
	for _, name := range sources {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		content, err := readInventoryFile(root, name)
		if err != nil {
			return nil, err
		}
		source, err := parser.ParseFile(token.NewFileSet(), name, content, parser.AllErrors)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrTypeAnalysis, err)
		}
		item, err := inspectGoSyntaxFile(name, source)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func inspectGoSyntaxFile(name string, source *ast.File) (GoSourceInventory, error) {
	result := GoSourceInventory{
		File: name, Package: source.Name.Name, Imports: make([]string, 0), Declarations: make([]string, 0),
	}
	for _, dependency := range source.Imports {
		value, err := strconv.Unquote(dependency.Path.Value)
		if err != nil {
			return GoSourceInventory{}, fmt.Errorf("decode import in %s: %w", name, err)
		}
		result.Imports = append(result.Imports, value)
	}
	for _, declaration := range source.Decls {
		result.Declarations = append(result.Declarations, syntaxDeclarationNames(declaration)...)
	}
	slices.Sort(result.Imports)
	slices.Sort(result.Declarations)
	return result, nil
}

func syntaxDeclarationNames(declaration ast.Decl) []string {
	result := make([]string, 0)
	switch value := declaration.(type) {
	case *ast.FuncDecl:
		result = append(result, value.Name.Name)
	case *ast.GenDecl:
		for _, spec := range value.Specs {
			switch specValue := spec.(type) {
			case *ast.TypeSpec:
				result = append(result, specValue.Name.Name)
			case *ast.ValueSpec:
				for _, name := range specValue.Names {
					result = append(result, name.Name)
				}
			}
		}
	}
	return result
}
