package architecture

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
)

func (proof *archiveExternalProof) value(
	frame *archiveExternalFrame, expression ast.Expr, depth int, initial bool,
) (*archiveExternalValue, error) {
	if expression == nil || depth > archiveOriginDepth {
		return nil, fmt.Errorf("%w: unresolved external value", errArchiveOrigin)
	}
	info := frame.function.pkg.TypesInfo
	valueType := info.TypeOf(expression)
	if valueType == nil || genericResourceType(archiveDeref(valueType)) {
		return nil, fmt.Errorf("%w: generic or unresolved external value", errArchiveOrigin)
	}
	if initial && archiveExternalDynamic(expression) {
		return nil, fmt.Errorf("%w: dynamic external initializer", errArchiveOrigin)
	}

	if archiveExternalPure(valueType) {
		return proof.zero(valueType, depth+1)
	}
	switch node := expression.(type) {
	case *ast.ParenExpr:
		return proof.value(frame, node.X, depth+1, initial)
	case *ast.Ident:
		return proof.identifier(frame, node, depth+1, initial)
	case *ast.SelectorExpr:
		return proof.selection(frame, node, depth+1, initial)
	case *ast.CompositeLit:
		return proof.literal(frame, node, depth+1, initial)
	case *ast.UnaryExpr:
		return proof.address(frame, node, depth, initial)
	case *ast.CallExpr:
		if initial {
			break
		}
		return proof.callValue(frame, node, depth+1)
	}
	return nil, fmt.Errorf("%w: unsupported external value expression", errArchiveOrigin)
}

func archiveExternalDynamic(expression ast.Expr) bool {
	dynamic := false
	ast.Inspect(expression, func(node ast.Node) bool {
		if _, ok := node.(*ast.CallExpr); ok {
			dynamic = true
		}
		return !dynamic
	})
	return dynamic
}

func (proof *archiveExternalProof) address(
	frame *archiveExternalFrame, node *ast.UnaryExpr, depth int, initial bool,
) (*archiveExternalValue, error) {
	if node.Op != token.AND {
		return nil, fmt.Errorf("%w: unsupported external unary value", errArchiveOrigin)
	}
	value, err := proof.value(frame, node.X, depth+1, initial)
	if err != nil {
		return nil, err
	}
	return &archiveExternalValue{valueType: frame.function.pkg.TypesInfo.TypeOf(node), fields: value.fields}, nil
}

func (proof *archiveExternalProof) identifier(
	frame *archiveExternalFrame, name *ast.Ident, depth int, initial bool,
) (*archiveExternalValue, error) {
	object := frame.function.pkg.TypesInfo.ObjectOf(name)
	if value := frame.bindings[object]; value != nil {
		if len(frame.function.locals[object]) != 0 || proof.origin.mutated[object] {
			return nil, fmt.Errorf("%w: external parameter was reassigned or escaped", errArchiveOrigin)
		}
		return value, nil
	}
	if global, ok := object.(*types.Var); ok && global.Pkg() != nil && global.Parent() == global.Pkg().Scope() {
		if initial {
			proof.allowInitializerUse(global, name)
		}
		return proof.global(global, depth+1)
	}
	assignments := frame.function.locals[object]
	if len(assignments) != 1 || proof.origin.mutated[object] {
		return nil, fmt.Errorf("%w: ambiguous external local value", errArchiveOrigin)
	}
	return proof.value(frame, assignments[0].node, depth+1, initial)
}

func (proof *archiveExternalProof) selection(
	frame *archiveExternalFrame, node *ast.SelectorExpr, depth int, initial bool,
) (*archiveExternalValue, error) {
	info := frame.function.pkg.TypesInfo
	if selected := info.Selections[node]; selected != nil {
		if selected.Kind() != types.FieldVal {
			return nil, fmt.Errorf("%w: external method value escaped", errArchiveOrigin)
		}
		value, err := proof.value(frame, node.X, depth+1, initial)
		if err != nil {
			return nil, err
		}
		return proof.fieldPath(value, selected.Index())
	}
	global, ok := info.Uses[node.Sel].(*types.Var)
	if !ok || global.Pkg() == nil || global.Parent() != global.Pkg().Scope() {
		return nil, fmt.Errorf("%w: unresolved external selection", errArchiveOrigin)
	}
	if initial {
		proof.allowInitializerUse(global, node.Sel)
	}
	return proof.global(global, depth+1)
}

func (proof *archiveExternalProof) fieldPath(
	value *archiveExternalValue, indexes []int,
) (*archiveExternalValue, error) {
	for _, index := range indexes {
		fields, ok := archiveDeref(value.valueType).Underlying().(*types.Struct)
		if !ok || index >= fields.NumFields() {
			return nil, fmt.Errorf("%w: unresolved external receiver field", errArchiveOrigin)
		}
		field := fields.Field(index)
		if proof.origin.mutated[field] {
			return nil, fmt.Errorf("%w: external executable field was modified or escaped", errArchiveOrigin)
		}
		member := value.fields[field]
		if member == nil {
			var err error
			member, err = proof.zero(field.Type(), 0)
			if err != nil {
				return nil, err
			}
		}
		value = member
	}
	return value, nil
}

func (proof *archiveExternalProof) literal(
	frame *archiveExternalFrame, node *ast.CompositeLit, depth int, initial bool,
) (*archiveExternalValue, error) {
	valueType := frame.function.pkg.TypesInfo.TypeOf(node)
	if err := proof.concrete(valueType); err != nil {
		return nil, err
	}
	fields, ok := archiveDeref(valueType).Underlying().(*types.Struct)
	if !ok {
		return nil, fmt.Errorf("%w: executable external literal is not a closed struct", errArchiveOrigin)
	}
	result := &archiveExternalValue{valueType: valueType, fields: make(map[*types.Var]*archiveExternalValue)}
	for index := range fields.NumFields() {
		field := fields.Field(index)
		expression := archiveExternalFieldExpression(frame.function.pkg.TypesInfo, node, field, index)
		var value *archiveExternalValue
		var err error
		if expression == nil {
			value, err = proof.zero(field.Type(), depth+1)
		} else {
			value, err = proof.value(frame, expression, depth+1, initial)
		}
		if err != nil {
			return nil, err
		}
		result.fields[field] = value
	}
	return result, nil
}

func archiveExternalFieldExpression(info *types.Info, node *ast.CompositeLit, field *types.Var, index int) ast.Expr {
	for ordinal, item := range node.Elts {
		pair, keyed := item.(*ast.KeyValueExpr)
		if !keyed {
			if ordinal == index {
				return item
			}
			continue
		}
		name, ok := pair.Key.(*ast.Ident)
		if ok && info.ObjectOf(name) == field {
			return pair.Value
		}
	}
	return nil
}

func (proof *archiveExternalProof) concrete(value types.Type) error {
	named, ok := archiveDeref(value).(*types.Named)
	if !ok || genericResourceType(named) {
		return fmt.Errorf("%w: external concrete type is unresolved or generic", errArchiveOrigin)
	}
	if _, err := proof.packageFor(named.Obj()); err != nil {
		return err
	}
	if _, ok := named.Underlying().(*types.Interface); ok {
		return fmt.Errorf("%w: external interface binding is unresolved", errArchiveOrigin)
	}
	return nil
}

func (proof *archiveExternalProof) zero(value types.Type, depth int) (*archiveExternalValue, error) {
	if depth > archiveOriginDepth || !archiveExternalPure(value) {
		return nil, fmt.Errorf("%w: missing external executable field origin", errArchiveOrigin)
	}
	return &archiveExternalValue{valueType: value}, nil
}

func (proof *archiveExternalProof) allowInitializerUse(global *types.Var, node ast.Node) {
	if proof.allowed[global] == nil {
		proof.allowed[global] = make(map[ast.Node]bool)
	}
	proof.allowed[global][node] = true
}

func (proof *archiveExternalProof) callValue(
	frame *archiveExternalFrame, call *ast.CallExpr, depth int,
) (*archiveExternalValue, error) {
	target, err := proof.bindCall(frame, call, depth+1)
	if err != nil {
		return nil, err
	}
	if target.target == nil {
		return nil, fmt.Errorf("%w: opaque external constructor return", errArchiveOrigin)
	}
	if err := proof.checkFrame(target.target, depth+1); err != nil {
		return nil, err
	}
	if len(target.target.function.returns) != 1 || len(target.target.function.returns[0].node.Results) != 1 {
		return nil, fmt.Errorf("%w: external constructor needs one direct result", errArchiveOrigin)
	}
	return proof.value(target.target, target.target.function.returns[0].node.Results[0], depth+1, false)
}
