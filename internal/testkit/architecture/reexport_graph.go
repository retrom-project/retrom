package architecture

import (
	"go/ast"
	"go/token"
	"go/types"
	"path"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// ReexportInventory identifies a forwarding declaration and its actual Model definitions.
type ReexportInventory struct {
	Symbol    string        `json:"symbol"`
	File      string        `json:"file"`
	Line      int           `json:"line"`
	Kind      string        `json:"kind"`
	Targets   []string      `json:"targets"`
	Consumers []GoReference `json:"consumers"`
}

type forwardingDeclaration struct {
	object  types.Object
	targets []types.Object
	file    string
	line    int
	kind    string
}

func inspectReexports(
	root string, graph []*packages.Package, registry OwnershipRegistry,
) ([]ReexportInventory, []Violation) {
	declarations := forwardingDeclarations(root, graph)
	edges := make(map[types.Object][]types.Object, len(declarations))
	for _, declaration := range declarations {
		edges[declaration.object] = declaration.targets
	}
	owners := make(map[string]PackageOwnership, len(registry.Packages))
	for _, owner := range registry.Packages {
		owners[owner.Path] = owner
	}
	records := make([]ReexportInventory, 0)
	violations := make([]Violation, 0)
	for _, declaration := range declarations {
		if owners[path.Dir(declaration.file)].Layer != "service" {
			continue
		}
		targets := modelForwardingTargets(declaration.object, edges, make(map[types.Object]bool))
		if len(targets) == 0 {
			continue
		}
		slices.Sort(targets)
		targets = slices.Compact(targets)
		symbol := inventoryObjectID(declaration.object)
		records = append(records, ReexportInventory{
			Symbol: symbol, File: declaration.file, Line: declaration.line, Kind: declaration.kind,
			Targets: targets, Consumers: []GoReference{},
		})
		violations = append(violations, Violation{
			Rule: "AR08", File: declaration.file, Line: declaration.line, Symbol: symbol,
			DependencyChain: append([]string{symbol}, targets...),
			Message:         "Service forwards Model definitions through " + declaration.kind,
		})
	}
	return records, violations
}

func forwardingDeclarations(root string, graph []*packages.Package) []forwardingDeclaration {
	result := make([]forwardingDeclaration, 0)
	for _, pkg := range graph {
		if pkg.ID != pkg.PkgPath {
			continue
		}
		for _, file := range pkg.Syntax {
			position := inventoryPosition(root, pkg.Fset, file.Pos())
			if strings.HasSuffix(position.Filename, "_test.go") {
				continue
			}
			for _, declaration := range file.Decls {
				result = append(result, inspectForwardingDeclaration(root, pkg, declaration)...)
			}
		}
	}
	return result
}

func inspectForwardingDeclaration(
	root string, pkg *packages.Package, declaration ast.Decl,
) []forwardingDeclaration {
	result := make([]forwardingDeclaration, 0)
	switch value := declaration.(type) {
	case *ast.GenDecl:
		for _, spec := range value.Specs {
			result = append(result, inspectForwardingSpec(root, pkg, value.Tok, spec)...)
		}
	case *ast.FuncDecl:
		result = append(result, forwardingRecord(root, pkg, value.Name, "empty wrapper",
			wrapperTargets(pkg.TypesInfo, value))...)
	}
	return result
}

func inspectForwardingSpec(
	root string, pkg *packages.Package, kind token.Token, spec ast.Spec,
) []forwardingDeclaration {
	result := make([]forwardingDeclaration, 0)
	switch item := spec.(type) {
	case *ast.TypeSpec:
		if item.Assign.IsValid() {
			result = append(result, forwardingRecord(root, pkg, item.Name, "type alias",
				referencedTypes(pkg.TypesInfo, item.Type))...)
		}
	case *ast.ValueSpec:
		if (kind == token.VAR || kind == token.CONST) && len(item.Names) == len(item.Values) {
			for index, expression := range item.Values {
				result = append(result, forwardingRecord(root, pkg, item.Names[index], "value forwarding",
					directForwardedObject(pkg.TypesInfo, expression))...)
			}
		}
	}
	return result
}

func forwardingRecord(
	root string, pkg *packages.Package, name *ast.Ident, kind string, targets []types.Object,
) []forwardingDeclaration {
	object := pkg.TypesInfo.Defs[name]
	if object == nil || len(targets) == 0 {
		return nil
	}
	position := inventoryPosition(root, pkg.Fset, name.Pos())
	return []forwardingDeclaration{{
		object: object, targets: targets, kind: kind, file: position.Filename, line: position.Line,
	}}
}

func directForwardedObject(info *types.Info, expression ast.Expr) []types.Object {
	object := callObject(info, expression)
	if object == nil || object.Pkg() == nil {
		return nil
	}
	return []types.Object{object}
}

func referencedTypes(info *types.Info, expression ast.Expr) []types.Object {
	result := make([]types.Object, 0)
	ast.Inspect(expression, func(node ast.Node) bool {
		name, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		if object, ok := info.ObjectOf(name).(*types.TypeName); ok && object.Pkg() != nil {
			result = append(result, object)
		}
		return true
	})
	return result
}

func modelForwardingTargets(
	object types.Object, edges map[types.Object][]types.Object, visited map[types.Object]bool,
) []string {
	if visited[object] {
		return nil
	}
	visited[object] = true
	if object.Pkg() != nil && strings.HasPrefix(object.Pkg().Path(), "retrom/internal/model/") {
		return []string{inventoryObjectID(object)}
	}
	result := make([]string, 0)
	for _, target := range edges[object] {
		result = append(result, modelForwardingTargets(target, edges, visited)...)
	}
	return result
}
