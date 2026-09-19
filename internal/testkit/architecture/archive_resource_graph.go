package architecture

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

var errArchiveOrigin = errors.New("unresolved archive resource origin")

const archiveOriginDepth = 40

type archiveFunction struct {
	object   *types.Func
	pkg      *packages.Package
	node     *ast.FuncDecl
	owner    PackageOwnership
	location ResourceLocation
	locals   map[types.Object][]archiveOperand
	returns  []archiveReturn
	calls    []archiveCall
}

type archiveReturn struct {
	node   *ast.ReturnStmt
	nonNil map[types.Object]bool
}

type archiveCall struct {
	node   *ast.CallExpr
	caller *archiveFunction
	target *types.Func
}

type archiveFrame struct {
	function *archiveFunction
	bindings map[types.Object]archiveOperand
}

type archiveOperand struct {
	node   ast.Expr
	frame  *archiveFrame
	index  int
	values []archiveValue
}

type archiveValue struct {
	valueType types.Type
	literal   *ast.CompositeLit
	frame     *archiveFrame
	location  ResourceLocation
	isNil     bool
}

type archiveOriginGraph struct {
	contract       archiveContract
	functions      map[*types.Func]*archiveFunction
	callers        map[*types.Func][]archiveCall
	roots          map[*types.Func]bool
	reachable      map[*types.Func]bool
	sources        map[string]bool
	mutated        map[types.Object]bool
	externalWrites map[types.Object]bool
	originUses     map[types.Object]*archiveOriginUse
	originOrder    []types.Object
}

type archiveOriginUse struct {
	function  *archiveFunction
	positions map[token.Pos]bool
}

func newArchiveOriginGraph(contract archiveContract) *archiveOriginGraph {
	graph := &archiveOriginGraph{
		contract: contract, functions: make(map[*types.Func]*archiveFunction),
		callers: make(map[*types.Func][]archiveCall), roots: make(map[*types.Func]bool),
		reachable: make(map[*types.Func]bool), sources: make(map[string]bool),
		mutated: make(map[types.Object]bool), externalWrites: make(map[types.Object]bool),
		originUses: make(map[types.Object]*archiveOriginUse),
	}
	for _, pkg := range contract.graph {
		if pkg.ID != pkg.PkgPath {
			continue
		}
		for _, file := range pkg.Syntax {
			graph.collectFile(pkg, file)
		}
	}
	for _, function := range graph.functions {
		graph.collectFunction(function)
	}
	graph.markReachable()
	return graph
}

func (graph *archiveOriginGraph) collectFile(pkg *packages.Package, file *ast.File) {
	location := inventoryPosition(graph.contract.root, pkg.Fset, file.Pos())
	owner := graph.contract.owners[path.Dir(location.Filename)]
	if strings.HasSuffix(location.Filename, "_test.go") || owner.Layer == "" ||
		owner.Layer == "testkit" || owner.Layer == "tool" {
		return
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		object, ok := pkg.TypesInfo.Defs[function.Name].(*types.Func)
		if !ok {
			continue
		}
		record := &archiveFunction{
			object: object.Origin(), pkg: pkg, node: function, owner: owner,
			location: graph.location(pkg, object.Pos(), inventoryObjectID(object)),
			locals:   make(map[types.Object][]archiveOperand),
		}
		graph.functions[object.Origin()] = record
		if owner.Layer == "cmd" && pkg.Name == "main" && function.Name.Name == "main" {
			graph.roots[object.Origin()] = true
		}
	}
}

func (graph *archiveOriginGraph) collectFunction(function *archiveFunction) {
	ast.Inspect(function.node.Body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.FuncLit:
			graph.collectClosureMutations(function, value)
			return false // An uninvoked closure is not construction evidence.
		case *ast.UnaryExpr:
			if value.Op == token.AND {
				graph.noteArchiveMutation(function, value.X)
			}
		case *ast.AssignStmt:
			graph.collectAssignments(function, value.Lhs, value.Rhs)
		case *ast.ValueSpec:
			left := make([]ast.Expr, 0, len(value.Names))
			for _, name := range value.Names {
				left = append(left, name)
			}
			graph.collectAssignments(function, left, value.Values)
		case *ast.CallExpr:
			object, ok := callObject(function.pkg.TypesInfo, value.Fun).(*types.Func)
			if ok && !function.pkg.TypesInfo.Types[value.Fun].IsType() {
				call := archiveCall{node: value, caller: function, target: object.Origin()}
				function.calls = append(function.calls, call)
				graph.callers[call.target] = append(graph.callers[call.target], call)
			}
		}
		return true
	})
	function.returns = archiveReturns(function.pkg.TypesInfo, function.node.Body, nil)
}

func (graph *archiveOriginGraph) collectAssignments(function *archiveFunction, left, right []ast.Expr) {
	for index, expression := range left {
		name, ok := expression.(*ast.Ident)
		if !ok {
			graph.noteArchiveMutation(function, expression)
			continue
		}
		object := function.pkg.TypesInfo.ObjectOf(name)
		operand := archiveOperand{index: index}
		switch {
		case len(right) == len(left):
			operand.node, operand.index = right[index], 0
		case len(right) == 1:
			operand.node = right[0]
		}
		function.locals[object] = append(function.locals[object], operand)
	}
}

func archiveReturns(
	info *types.Info, body ast.Node, nonNil map[types.Object]bool,
) []archiveReturn {
	var result []archiveReturn
	ast.Inspect(body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			result = append(result, archiveReturn{node: value, nonNil: nonNil})
		case *ast.IfStmt:
			yes, no := archiveNilGuard(info, value.Cond, nonNil)
			result = append(result, archiveReturns(info, value.Body, yes)...)
			if value.Else != nil {
				result = append(result, archiveReturns(info, value.Else, no)...)
			}
			return false
		}
		return true
	})
	return result
}

func archiveNilGuard(
	info *types.Info, condition ast.Expr, known map[types.Object]bool,
) (map[types.Object]bool, map[types.Object]bool) {
	expression, ok := condition.(*ast.BinaryExpr)
	if !ok || expression.Op != token.NEQ && expression.Op != token.EQL {
		return known, known
	}
	name, identifier := expression.X.(*ast.Ident)
	if !identifier || !archiveNil(info, expression.Y) {
		return known, known
	}
	nonNil := make(map[types.Object]bool, len(known)+1)
	for object, value := range known {
		nonNil[object] = value
	}
	nonNil[info.ObjectOf(name)] = true
	if expression.Op == token.NEQ {
		return nonNil, known
	}
	return known, nonNil
}

func (graph *archiveOriginGraph) markReachable() {
	var pending []*types.Func
	for root := range graph.roots {
		pending = append(pending, root)
	}
	for len(pending) != 0 {
		object := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		function := graph.functions[object]
		if graph.reachable[object] || function == nil {
			continue
		}
		graph.reachable[object] = true
		for _, call := range function.calls {
			pending = append(pending, call.target)
		}
	}
}

func (graph *archiveOriginGraph) location(
	pkg *packages.Package, position token.Pos, symbol string,
) ResourceLocation {
	location := inventoryPosition(graph.contract.root, pkg.Fset, position)
	return ResourceLocation{Symbol: symbol, File: location.Filename, Line: location.Line}
}

func (graph *archiveOriginGraph) note(function *archiveFunction) {
	graph.sources[function.location.File] = true
}

func (graph *archiveOriginGraph) snapshot() ([]SourceFile, error) {
	names := make([]string, 0, len(graph.sources))
	for name := range graph.sources {
		names = append(names, name)
	}
	slices.Sort(names)
	snapshot, err := SnapshotFiles(graph.contract.root, names)
	if err != nil {
		return nil, fmt.Errorf("archive binding evidence: %w", err)
	}
	return snapshot.Files, nil
}

func (graph *archiveOriginGraph) collectClosureMutations(function *archiveFunction, literal *ast.FuncLit) {
	ast.Inspect(literal.Body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.AssignStmt:
			for _, left := range value.Lhs {
				graph.noteArchiveMutation(function, left)
			}
		case *ast.UnaryExpr:
			if value.Op == token.AND {
				graph.noteArchiveMutation(function, value.X)
			}
		}
		return true
	})
}

func (graph *archiveOriginGraph) noteArchiveMutation(function *archiveFunction, expression ast.Expr) {
	info := function.pkg.TypesInfo
	var object types.Object
	switch value := expression.(type) {
	case *ast.Ident:
		object = info.ObjectOf(value)
	case *ast.SelectorExpr:
		object = info.ObjectOf(value.Sel)
	case *ast.ParenExpr:
		graph.noteArchiveMutation(function, value.X)
	case *ast.StarExpr:
		graph.noteArchiveMutation(function, value.X)
		graph.noteArchiveWholeWrite(function, info.TypeOf(value))
	}
	graph.noteArchiveWrittenObject(function, object)
}

func (graph *archiveOriginGraph) noteArchiveWholeWrite(function *archiveFunction, value types.Type) {
	if value == nil {
		return
	}
	fields, ok := archiveDeref(value).Underlying().(*types.Struct)
	if !ok {
		return
	}
	// Whole-object replacement invalidates every field origin, including writes
	// through aliases and helper parameters. This intentionally rejects all
	// instances of this declaration instead of guessing pointer identities.
	for index := range fields.NumFields() {
		graph.noteArchiveWrittenObject(function, fields.Field(index))
	}
}

func (graph *archiveOriginGraph) noteArchiveWrittenObject(function *archiveFunction, object types.Object) {
	if object == nil {
		return
	}
	graph.mutated[object] = true
	if field, ok := object.(*types.Var); ok && field.IsField() && function.owner.Layer != "adapter" {
		graph.externalWrites[object] = true
	}
}

func archiveNil(info *types.Info, expression ast.Expr) bool {
	name, ok := expression.(*ast.Ident)
	return ok && info.ObjectOf(name) == types.Universe.Lookup("nil")
}

func archiveOriginError(function *archiveFunction, node ast.Node, reason string) error {
	line := function.location.Line
	if node != nil {
		line = function.pkg.Fset.Position(node.Pos()).Line
	}
	return fmt.Errorf("%w: %s:%d: %s", errArchiveOrigin, function.location.File, line, reason)
}
