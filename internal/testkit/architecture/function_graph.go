package architecture

import (
	"go/ast"
	"go/token"
	"go/types"
	"path"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// FunctionInventory records resolved execution edges, not text matches for names.
type FunctionInventory struct {
	Symbol string          `json:"symbol"`
	File   string          `json:"file"`
	Line   int             `json:"line"`
	Layer  string          `json:"layer"`
	Module string          `json:"module"`
	Owner  string          `json:"owner"`
	Calls  []CallInventory `json:"calls"`
}

// CallInventory distinguishes dynamic calls, known definitions and direct effects.
type CallInventory struct {
	Symbol  string        `json:"symbol"`
	Package string        `json:"package"`
	Line    int           `json:"line"`
	Effect  string        `json:"effect,omitempty"`
	SQL     *SQLInventory `json:"sql,omitempty"`
}

func inspectFunctionGraph(
	root string, graph []*packages.Package, registry OwnershipRegistry,
) []FunctionInventory {
	owners := make(map[string]PackageOwnership)
	for _, owner := range registry.Packages {
		owners[owner.Path] = owner
	}
	functions := make([]FunctionInventory, 0)
	for _, pkg := range graph {
		if pkg.ID != pkg.PkgPath {
			continue
		}
		for _, file := range pkg.Syntax {
			position := inventoryPosition(root, pkg.Fset, file.Pos())
			if strings.HasSuffix(position.Filename, "_test.go") {
				continue
			}
			owner := owners[path.Dir(position.Filename)]
			functions = append(functions, inspectFileFunctions(root, pkg, file, owner)...)
		}
	}
	slices.SortFunc(functions, func(left, right FunctionInventory) int {
		return strings.Compare(left.Symbol+"\x00"+left.File, right.Symbol+"\x00"+right.File)
	})
	return functions
}

func inspectFileFunctions(
	root string, pkg *packages.Package, file *ast.File, owner PackageOwnership,
) []FunctionInventory {
	functions := make([]FunctionInventory, 0, len(file.Decls))
	for _, declaration := range file.Decls {
		position := inventoryPosition(root, pkg.Fset, declaration.Pos())
		item := FunctionInventory{
			File: position.Filename, Line: position.Line, Layer: owner.Layer, Module: owner.Module, Owner: owner.Owner,
			Calls: make([]CallInventory, 0),
		}
		var body ast.Node
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			if typed.Body == nil {
				continue
			}
			item.Symbol = inventoryObjectID(pkg.TypesInfo.Defs[typed.Name])
			if typed.Name.Name == "init" {
				item.Symbol += "@" + item.File + ":" + strconv.Itoa(item.Line)
			}
			body = typed.Body
		case *ast.GenDecl:
			if typed.Tok != token.VAR {
				continue
			}
			item.Symbol = pkg.PkgPath + ".<init>@" + item.File + ":" + strconv.Itoa(item.Line)
			body = typed
		default:
			continue
		}
		item.Calls = executionEdges(pkg, body)
		functions = append(functions, item)
	}
	return functions
}

func executionEdges(pkg *packages.Package, body ast.Node) []CallInventory {
	calls := make([]CallInventory, 0)
	ast.Inspect(body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.CallExpr:
			if pkg.TypesInfo.Types[value.Fun].IsType() {
				return true
			}
			calls = append(calls, inspectCall(pkg, value))
		case *ast.GoStmt:
			calls = append(calls, CallInventory{
				Symbol: "go", Line: pkg.Fset.Position(value.Pos()).Line, Effect: "goroutine",
			})
		case *ast.SelectorExpr:
			object := pkg.TypesInfo.ObjectOf(value.Sel)
			if function, ok := object.(*types.Func); ok && sqlExecutionMethod(function) {
				calls = append(calls, CallInventory{
					Symbol: inventoryObjectID(function), Package: function.Pkg().Path(),
					Line: pkg.Fset.Position(value.Pos()).Line, Effect: "SQL execution capability",
				})
			}
		}
		return true
	})
	return calls
}

func inspectCall(pkg *packages.Package, call *ast.CallExpr) CallInventory {
	item := CallInventory{Line: pkg.Fset.Position(call.Pos()).Line}
	if _, literal := call.Fun.(*ast.FuncLit); literal {
		item.Symbol = "<literal>"
		return item
	}
	object := callObject(pkg.TypesInfo, call.Fun)
	if object == nil {
		item.Symbol, item.Effect = "<dynamic>", "unresolved executable value"
		return item
	}
	item.Symbol = object.Name()
	if object.Pkg() != nil {
		item.Symbol, item.Package = inventoryObjectID(object), object.Pkg().Path()
	}
	function, ok := object.(*types.Func)
	if ok {
		item.Effect = callEffect(function)
		item.SQL = inspectSQLArgument(pkg.TypesInfo, call, function)
	} else if builtin, ok := object.(*types.Builtin); ok {
		if builtin.Name() == "print" || builtin.Name() == "println" {
			item.Effect = "process output"
		}
	} else {
		item.Effect = "unresolved executable value"
	}
	return item
}

func callObject(info *types.Info, expression ast.Expr) types.Object {
	switch value := expression.(type) {
	case *ast.Ident:
		return info.ObjectOf(value)
	case *ast.SelectorExpr:
		return info.ObjectOf(value.Sel)
	case *ast.IndexExpr:
		return callObject(info, value.X)
	case *ast.IndexListExpr:
		return callObject(info, value.X)
	case *ast.ParenExpr:
		return callObject(info, value.X)
	default:
		return nil
	}
}
