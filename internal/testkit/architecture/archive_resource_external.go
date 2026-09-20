package architecture

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path"
	"strings"

	"golang.org/x/tools/go/packages"
)

// ArchiveExternalSource binds technical implementation evidence to a verified module.
type ArchiveExternalSource struct {
	Module  string `json:"module"`
	Version string `json:"version"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
}

type archiveExternalValue struct {
	valueType types.Type
	fields    map[*types.Var]*archiveExternalValue
}

type archiveExternalFrame struct {
	function *archiveFunction
	bindings map[types.Object]*archiveExternalValue
}

type archiveExternalProof struct {
	origin    *archiveOriginGraph
	packages  map[*types.Package]*packages.Package
	parents   map[*packages.Package]map[ast.Node]ast.Node
	functions map[*types.Func]*archiveFunction
	modules   map[string]string
	sources   map[string]ArchiveExternalSource
	globals   map[*types.Var]*archiveExternalValue
	pending   map[*types.Var]bool
	allowed   map[*types.Var]map[ast.Node]bool
	checked   map[string]bool
	active    map[*types.Func]bool
}

func newArchiveExternalProof(origin *archiveOriginGraph) *archiveExternalProof {
	proof := &archiveExternalProof{
		origin:    origin,
		packages:  make(map[*types.Package]*packages.Package),
		parents:   make(map[*packages.Package]map[ast.Node]ast.Node),
		functions: make(map[*types.Func]*archiveFunction),
		modules:   make(map[string]string),
		sources:   make(map[string]ArchiveExternalSource),
		globals:   make(map[*types.Var]*archiveExternalValue),
		pending:   make(map[*types.Var]bool),
		allowed:   make(map[*types.Var]map[ast.Node]bool),
		checked:   make(map[string]bool),
		active:    make(map[*types.Func]bool),
	}

	packages.Visit(origin.contract.graph, func(pkg *packages.Package) bool {
		if pkg.ID == pkg.PkgPath && pkg.Types != nil && pkg.TypesInfo != nil {
			proof.packages[pkg.Types] = pkg
		}
		return true
	}, nil)
	return proof
}

func (proof *archiveExternalProof) check(function *archiveFunction, name *ast.Ident, global *types.Var) error {
	const reason = "package-level executable or resource state has no owned origin: "
	if _, err := proof.global(global, 0); err != nil {
		return archiveOriginError(function, name, reason+err.Error())
	}
	for dependency := range proof.globals {
		if err := proof.auditGlobal(dependency, make(map[*types.Var]bool)); err != nil {
			return archiveOriginError(function, name, reason+err.Error())
		}
	}
	return nil
}

func (proof *archiveExternalProof) packageFor(object types.Object) (*packages.Package, error) {
	pkg := proof.packages[object.Pkg()]
	if pkg == nil || len(pkg.Syntax) == 0 || pkg.TypesInfo == nil {
		return nil, fmt.Errorf("%w: missing compiled external declaration", errArchiveOrigin)
	}
	if err := proof.verifyModule(pkg); err != nil {
		return nil, err
	}
	return pkg, nil
}

func (proof *archiveExternalProof) parentMap(pkg *packages.Package) map[ast.Node]ast.Node {
	if result := proof.parents[pkg]; result != nil {
		return result
	}
	result := make(map[ast.Node]ast.Node)
	for _, file := range pkg.Syntax {
		var stack []ast.Node
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			if len(stack) > 0 {
				result[node] = stack[len(stack)-1]
			}
			stack = append(stack, node)
			return true
		})
	}
	proof.parents[pkg] = result
	return result
}

func (proof *archiveExternalProof) global(global *types.Var, depth int) (*archiveExternalValue, error) {
	if depth > archiveOriginDepth || proof.pending[global] {
		return nil, fmt.Errorf("%w: cyclic external initializer", errArchiveOrigin)
	}
	if value := proof.globals[global]; value != nil {
		return value, nil
	}
	if archiveTechnicalType(global.Type()) || archiveGlobalStream(global.Type()) || genericResourceType(global.Type()) {
		return nil, fmt.Errorf("%w: external global is a resource or generic value", errArchiveOrigin)
	}
	pkg, err := proof.packageFor(global)
	if err != nil {
		return nil, err
	}
	expression, err := proof.initializer(pkg, global)
	if err != nil {
		return nil, err
	}
	proof.pending[global] = true
	defer delete(proof.pending, global)
	frame := &archiveExternalFrame{
		function: proof.globalFunction(pkg, expression), bindings: make(map[types.Object]*archiveExternalValue),
	}
	value, err := proof.value(frame, expression, depth+1, true)
	if err != nil {
		return nil, err
	}
	if err := proof.noteExternal(pkg, global.Pos()); err != nil {
		return nil, err
	}
	proof.globals[global] = value
	return value, nil
}

func (proof *archiveExternalProof) initializer(pkg *packages.Package, global *types.Var) (ast.Expr, error) {
	for name, object := range pkg.TypesInfo.Defs {
		if object != global {
			continue
		}
		declaration, ok := proof.parentMap(pkg)[name].(*ast.ValueSpec)
		if !ok || len(declaration.Names) != len(declaration.Values) {
			break
		}
		for index, candidate := range declaration.Names {
			if candidate == name {
				return declaration.Values[index], nil
			}
		}
	}
	return nil, fmt.Errorf("%w: external initializer is not a direct value declaration", errArchiveOrigin)
}

func (proof *archiveExternalProof) globalFunction(pkg *packages.Package, node ast.Node) *archiveFunction {
	position := pkg.Fset.Position(node.Pos())
	return &archiveFunction{
		pkg: pkg, location: ResourceLocation{File: position.Filename, Line: position.Line},
		locals: make(map[types.Object][]archiveOperand),
	}
}

func (proof *archiveExternalProof) declaration(object *types.Func) (*archiveFunction, error) {
	object = object.Origin()
	if value := proof.functions[object]; value != nil {
		return value, nil
	}
	if len(proof.functions) >= 64 {
		return nil, fmt.Errorf("%w: external method proof limit", errArchiveOrigin)
	}
	pkg, err := proof.packageFor(object)
	if err != nil {
		return nil, err
	}
	for _, file := range pkg.Syntax {
		for _, item := range file.Decls {
			node, ok := item.(*ast.FuncDecl)
			if !ok || pkg.TypesInfo.Defs[node.Name] != object || node.Body == nil {
				continue
			}
			function := proof.globalFunction(pkg, node)
			function.node, function.object = node, object
			proof.origin.collectAssignmentsForExternal(function)
			proof.functions[object] = function
			if err := proof.noteExternal(pkg, object.Pos()); err != nil {
				return nil, err
			}
			return function, nil
		}
	}
	return nil, fmt.Errorf("%w: external method body unavailable", errArchiveOrigin)
}

func (graph *archiveOriginGraph) collectAssignmentsForExternal(function *archiveFunction) {
	ast.Inspect(function.node.Body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.UnaryExpr:
			if value.Op == token.AND {
				graph.noteArchiveMutation(function, value.X)
			}
		case *ast.AssignStmt:
			graph.collectAssignments(function, value.Lhs, value.Rhs)
		case *ast.ValueSpec:
			left := make([]ast.Expr, len(value.Names))
			for index, name := range value.Names {
				left[index] = name
			}
			graph.collectAssignments(function, left, value.Values)
		}
		return true
	})
	function.returns = archiveReturns(function.pkg.TypesInfo, function.node.Body, nil)
}

func archiveExternalPure(value types.Type) bool {
	return value != nil &&
		(types.Identical(value, types.Universe.Lookup("error").Type()) || len(InspectValueType(value)) == 0)
}

func archiveExternalResults(value types.Type) bool {
	if tuple, ok := value.(*types.Tuple); ok {
		if tuple.Len() == 0 {
			return false
		}
		for index := range tuple.Len() {
			if !archiveExternalPure(tuple.At(index).Type()) {
				return false
			}
		}
		return true
	}
	return archiveExternalPure(value)
}

func (proof *archiveExternalProof) productionFile(pkg *packages.Package, node ast.Node) bool {
	file := pkg.Fset.Position(node.Pos()).Filename
	if strings.HasSuffix(file, "_test.go") {
		return false
	}
	if pkg.Module != nil && !pkg.Module.Main {
		return true
	}
	position := inventoryPosition(proof.origin.contract.root, pkg.Fset, node.Pos())
	owner := proof.origin.contract.owners[path.Dir(position.Filename)]
	return owner.Layer != "" && owner.Layer != "testkit" && owner.Layer != "tool"
}

func archiveGlobalStream(value types.Type) bool {
	methods := types.NewMethodSet(value)
	for index := range methods.Len() {
		method, ok := methods.At(index).Obj().(*types.Func)
		if !ok {
			continue
		}
		candidate := types.NewInterfaceType([]*types.Func{method}, nil).Complete()
		if streamPort(candidate) || closeOnlyResource(candidate) {
			return true
		}
	}
	return false
}

func archiveGlobalExecution(value types.Type) bool {
	// Pure parameters and results do not prove that a method body has no executable dependencies.
	return types.NewMethodSet(value).Len() != 0 || types.NewMethodSet(types.NewPointer(value)).Len() != 0
}
