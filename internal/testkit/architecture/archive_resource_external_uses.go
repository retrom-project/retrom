package architecture

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/packages"
)

func (proof *archiveExternalProof) auditGlobal(global *types.Var, visiting map[*types.Var]bool) error {
	if visiting[global] {
		return nil
	}
	if len(visiting) >= archiveOriginDepth {
		return fmt.Errorf("%w: external alias proof limit", errArchiveOrigin)
	}
	visiting[global] = true
	for _, pkg := range proof.packages {
		for name, object := range pkg.TypesInfo.Uses {
			if object != global || !proof.productionFile(pkg, name) || proof.allowed[global][name] {
				continue
			}
			if err := proof.auditUse(pkg, name, visiting); err != nil {
				return err
			}
		}
	}
	return nil
}

func (proof *archiveExternalProof) auditUse(
	pkg *packages.Package, name *ast.Ident, visiting map[*types.Var]bool,
) error {
	if alias := proof.declaredAlias(pkg, name); alias != nil {
		if _, err := proof.packageFor(alias); err != nil {
			return err
		}
		if err := proof.noteExternal(pkg, alias.Pos()); err != nil {
			return err
		}
		return proof.auditAlias(alias, visiting)
	}
	call, err := proof.closedCall(pkg, name)
	if err != nil {
		return err
	}
	function, err := proof.enclosingFunction(pkg, call)
	if err != nil {
		return err
	}
	if pkg.Module != nil && pkg.Module.Main {
		proof.origin.note(function)
	} else if err := proof.noteExternal(pkg, name.Pos()); err != nil {
		return err
	}
	frame := &archiveExternalFrame{function: function, bindings: make(map[types.Object]*archiveExternalValue)}
	bound, err := proof.bindCall(frame, call, 0)
	if err != nil {
		return err
	}
	if bound.target == nil {
		return fmt.Errorf("%w: external global receiver was not bound", errArchiveOrigin)
	}
	return proof.checkFrame(bound.target, 0)
}

func (proof *archiveExternalProof) declaredAlias(pkg *packages.Package, name *ast.Ident) *types.Var {
	if pkg.Module == nil || pkg.Module.Main {
		return nil
	}
	parents := proof.parentMap(pkg)
	for node := ast.Node(name); node != nil; node = parents[node] {
		if _, ok := node.(*ast.FuncDecl); ok {
			return nil
		}
		if _, ok := node.(*ast.CallExpr); ok {
			return nil
		}
		if declaration, ok := node.(*ast.ValueSpec); ok {
			if len(declaration.Names) != 1 || len(declaration.Values) != 1 {
				return nil
			}
			global, ok := pkg.TypesInfo.Defs[declaration.Names[0]].(*types.Var)
			if ok && global.Parent() == global.Pkg().Scope() {
				return global
			}
			return nil
		}
	}
	return nil
}

func (proof *archiveExternalProof) auditAlias(global *types.Var, visiting map[*types.Var]bool) error {
	if visiting[global] {
		return nil
	}
	if len(visiting) >= archiveOriginDepth {
		return fmt.Errorf("%w: external alias proof limit", errArchiveOrigin)
	}
	visiting[global] = true
	for _, pkg := range proof.packages {
		for name, object := range pkg.TypesInfo.Uses {
			if object != global || !proof.productionFile(pkg, name) {
				continue
			}
			alias := proof.declaredAlias(pkg, name)
			if alias == nil {
				return fmt.Errorf("%w: external global alias has an unresolved use or escape", errArchiveOrigin)
			}
			if err := proof.auditAlias(alias, visiting); err != nil {
				return err
			}
		}
	}
	return nil
}

func (proof *archiveExternalProof) closedCall(pkg *packages.Package, name *ast.Ident) (*ast.CallExpr, error) {
	parents := proof.parentMap(pkg)
	var expression ast.Expr = name
	selected, _ := parents[name].(*ast.SelectorExpr)
	if selected != nil && selected.Sel == name && pkg.TypesInfo.Selections[selected] == nil {
		expression = selected
	}
	var result *ast.CallExpr
	for {
		selector, ok := parents[expression].(*ast.SelectorExpr)
		if !ok || selector.X != expression {
			break
		}
		selected := pkg.TypesInfo.Selections[selector]
		if selected == nil || selected.Kind() != types.MethodVal {
			break
		}
		call, ok := parents[selector].(*ast.CallExpr)
		if !ok || call.Fun != selector {
			break
		}
		signature, ok := selected.Obj().Type().(*types.Signature)
		if !ok {
			return nil, fmt.Errorf("%w: unresolved external boundary signature", errArchiveOrigin)
		}
		if !archiveExternalPureParameters(signature) {
			return nil, fmt.Errorf("%w: program injects an executable external argument", errArchiveOrigin)
		}
		result, expression = call, call
	}
	if result == nil || !archiveExternalResults(pkg.TypesInfo.TypeOf(result)) {
		return nil, fmt.Errorf("%w: external global is written, aliased or escapes a closed value call", errArchiveOrigin)
	}
	return result, nil
}

func (proof *archiveExternalProof) enclosingFunction(pkg *packages.Package, node ast.Node) (*archiveFunction, error) {
	parents := proof.parentMap(pkg)
	for current := node; current != nil; current = parents[current] {
		if _, ok := current.(*ast.FuncLit); ok {
			return nil, fmt.Errorf("%w: external global captured by a closure", errArchiveOrigin)
		}
		declaration, ok := current.(*ast.FuncDecl)
		if !ok {
			continue
		}
		object, ok := pkg.TypesInfo.Defs[declaration.Name].(*types.Func)
		if !ok {
			break
		}
		if function := proof.origin.functions[object.Origin()]; function != nil {
			return function, nil
		}
		return proof.declaration(object)
	}
	return nil, fmt.Errorf("%w: external global has no direct consuming function", errArchiveOrigin)
}
