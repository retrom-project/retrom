package architecture

import (
	"go/ast"
	"go/token"
	"go/types"
)

func (graph *archiveOriginGraph) resolve(operand archiveOperand, depth int) ([]archiveValue, error) {
	if operand.values != nil {
		return operand.values, nil
	}
	frame := operand.frame
	if frame == nil || frame.function == nil {
		return nil, errArchiveOrigin
	}
	graph.note(frame.function)
	if depth > archiveOriginDepth || operand.node == nil {
		return nil, archiveOriginError(frame.function, operand.node, "missing, cyclic or over-limit origin")
	}
	switch value := operand.node.(type) {
	case *ast.ParenExpr:
		operand.node = value.X
		return graph.resolve(operand, depth+1)
	case *ast.Ident:
		return graph.resolveIdentifier(operand, value, depth+1)
	case *ast.CompositeLit:
		return []archiveValue{{
			valueType: frame.function.pkg.TypesInfo.TypeOf(value), literal: value, frame: frame,
			location: graph.location(frame.function.pkg, value.Pos(), inventoryObjectID(frame.function.object)),
		}}, nil
	case *ast.UnaryExpr:
		if value.Op == token.AND {
			operand.node = value.X
			values, err := graph.resolve(operand, depth+1)
			for index := range values {
				values[index].valueType = frame.function.pkg.TypesInfo.TypeOf(value)
			}
			return values, err
		}
	case *ast.StarExpr:
		operand.node = value.X
		return graph.resolve(operand, depth+1)
	case *ast.SelectorExpr:
		return graph.resolveField(operand, value, depth+1)
	case *ast.CallExpr:
		return graph.resolveCall(operand, value, depth+1)
	}
	return nil, archiveOriginError(frame.function, operand.node, "unsupported origin expression")
}

func (graph *archiveOriginGraph) resolveIdentifier(
	operand archiveOperand, name *ast.Ident, depth int,
) ([]archiveValue, error) {
	function := operand.frame.function
	if archiveNil(function.pkg.TypesInfo, name) {
		return []archiveValue{{isNil: true}}, nil
	}
	object := function.pkg.TypesInfo.ObjectOf(name)
	graph.noteOriginUse(function, object, name.Pos())
	assignments := function.locals[object]
	if binding, exists := operand.frame.bindings[object]; exists && len(assignments) == 0 && !graph.mutated[object] {
		return graph.resolve(binding, depth+1)
	}
	if len(assignments) != 1 || graph.mutated[object] {
		return nil, archiveOriginError(function, name, "origin is unbound, reassigned or escapes by address")
	}
	local := assignments[0]
	local.frame = operand.frame
	return graph.resolve(local, depth+1)
}

func (graph *archiveOriginGraph) resolveField(
	operand archiveOperand, field *ast.SelectorExpr, depth int,
) ([]archiveValue, error) {
	function := operand.frame.function
	selection := function.pkg.TypesInfo.Selections[field]
	if selection == nil || selection.Kind() != types.FieldVal {
		return nil, archiveOriginError(function, field, "non-field selector origin")
	}
	values, err := graph.resolve(archiveOperand{node: field.X, frame: operand.frame}, depth+1)
	if err != nil {
		return nil, err
	}
	for _, index := range selection.Index() {
		var next []archiveValue
		for _, value := range values {
			resolved, err := graph.resolveFieldIndex(value, index, depth+1)
			if err != nil {
				return nil, err
			}
			next = append(next, resolved...)
		}
		values = next
	}
	return values, nil
}

func (graph *archiveOriginGraph) resolveFieldIndex(
	value archiveValue, index, depth int,
) ([]archiveValue, error) {
	if value.frame == nil || value.isNil {
		return nil, errArchiveOrigin
	}
	fields, ok := archiveDeref(value.valueType).Underlying().(*types.Struct)
	if !ok || index >= fields.NumFields() {
		return nil, archiveOriginError(value.frame.function, value.literal, "unknown field owner")
	}
	field := fields.Field(index)
	if graph.mutated[field] {
		return nil, archiveOriginError(value.frame.function, value.literal, "resource injection field is mutated")
	}
	expression := archiveLiteralField(value, index, field)
	if expression == nil {
		return nil, archiveOriginError(value.frame.function, value.literal, "injection field has no explicit origin")
	}
	return graph.resolve(archiveOperand{node: expression, frame: value.frame}, depth+1)
}

func archiveLiteralField(value archiveValue, index int, field *types.Var) ast.Expr {
	if value.literal == nil {
		return nil
	}
	for ordinal, element := range value.literal.Elts {
		pair, keyed := element.(*ast.KeyValueExpr)
		if !keyed {
			if ordinal == index {
				return element
			}
			continue
		}
		name, identifier := pair.Key.(*ast.Ident)
		if identifier && value.frame.function.pkg.TypesInfo.ObjectOf(name) == field {
			return pair.Value
		}
	}
	return nil
}

func (graph *archiveOriginGraph) resolveCall(
	operand archiveOperand, call *ast.CallExpr, depth int,
) ([]archiveValue, error) {
	function := operand.frame.function
	if function.pkg.TypesInfo.Types[call.Fun].IsType() && len(call.Args) == 1 {
		operand.node = call.Args[0]
		values, err := graph.resolve(operand, depth+1)
		destination := function.pkg.TypesInfo.TypeOf(call)
		if _, port := destination.Underlying().(*types.Interface); !port {
			for index := range values {
				values[index].valueType = destination
			}
		}
		return values, err // Interface conversions retain the underlying concrete methods.
	}
	object := callObject(function.pkg.TypesInfo, call.Fun)
	if builtin, ok := object.(*types.Builtin); ok && builtin.Name() == "new" && len(call.Args) == 1 {
		return []archiveValue{{
			valueType: function.pkg.TypesInfo.TypeOf(call), frame: operand.frame,
			location: graph.location(function.pkg, call.Pos(), inventoryObjectID(function.object)),
		}}, nil
	}
	target, ok := object.(*types.Func)
	if !ok {
		return nil, archiveOriginError(function, call, "dynamic constructor or factory callback")
	}
	frame, err := graph.callFrame(archiveCall{node: call, caller: function, target: target.Origin()}, operand.frame)
	if err != nil {
		return nil, err
	}
	return graph.resolveReturns(frame, operand.index, depth+1)
}

func (graph *archiveOriginGraph) callFrame(call archiveCall, parent *archiveFrame) (*archiveFrame, error) {
	return graph.bindFrame(call, parent, false)
}

func (graph *archiveOriginGraph) bridgeFrame(call archiveCall, parent *archiveFrame) (*archiveFrame, error) {
	return graph.bindFrame(call, parent, true)
}

func (graph *archiveOriginGraph) bindFrame(
	call archiveCall, parent *archiveFrame, bridge bool,
) (*archiveFrame, error) {
	target := graph.functions[call.target]
	if target == nil {
		return nil, archiveOriginError(call.caller, call.node, "constructor has no local production definition")
	}
	signature, ok := target.object.Type().(*types.Signature)
	if !ok || signature.TypeParams().Len() != 0 || signature.RecvTypeParams().Len() != 0 {
		return nil, archiveOriginError(call.caller, call.node, "unsupported constructor argument binding")
	}
	boundParameters, supported := archiveFrameParameterCount(signature, call.node, bridge)
	if !supported {
		return nil, archiveOriginError(call.caller, call.node, "unsupported constructor argument binding")
	}
	if err := graph.validateArchiveConstructor(call, target, signature); err != nil {
		return nil, err
	}
	frame := &archiveFrame{function: target, bindings: make(map[types.Object]archiveOperand)}
	for index := range boundParameters {
		frame.bindings[signature.Params().At(index)] = archiveOperand{node: call.node.Args[index], frame: parent}
	}
	if signature.Recv() != nil {
		selector, ok := call.node.Fun.(*ast.SelectorExpr)
		if !ok || call.caller.pkg.TypesInfo.Selections[selector] == nil ||
			call.caller.pkg.TypesInfo.Selections[selector].Kind() != types.MethodVal {
			return nil, archiveOriginError(call.caller, call.node, "unsupported method receiver binding")
		}
		frame.bindings[signature.Recv()] = archiveOperand{node: selector.X, frame: parent}
	}
	return frame, nil
}

func archiveFrameParameterCount(signature *types.Signature, call *ast.CallExpr, bridge bool) (int, bool) {
	bound := signature.Params().Len()
	if signature.Variadic() {
		if !bridge || call.Ellipsis.IsValid() {
			return 0, false
		}
		// Reachability through an ordinary caller does not prove its optional values.
		// Leave this slot unbound: demanding it later must fail in resolveIdentifier.
		// Actual factory/return construction always uses the strict callFrame path.
		bound--
	}
	return bound, len(call.Args) >= bound && (signature.Variadic() || len(call.Args) == bound)
}

func (graph *archiveOriginGraph) validateArchiveConstructor(
	call archiveCall, target *archiveFunction, signature *types.Signature,
) error {
	if target.owner.Layer != "adapter" {
		return nil
	}
	if call.caller.owner.Layer != "bootstrap" && call.caller.owner.Layer != "adapter" {
		return archiveOriginError(call.caller, call.node, "Adapter construction outside Bootstrap or its owner")
	}
	for index := range signature.Params().Len() {
		value := signature.Params().At(index).Type()
		if reason := graph.archiveDependency(value, make(map[types.Type]bool)); reason != "" {
			return archiveOriginError(call.caller, call.node, reason)
		}
		if call.caller.owner.Layer == "bootstrap" && graph.externalArchiveInput(value, make(map[types.Type]bool)) {
			return archiveOriginError(call.caller, call.node, "external resource injection has no proven owned origin")
		}
	}
	return graph.checkArchiveEffects(target, make(map[*types.Func]bool))
}

func (graph *archiveOriginGraph) resolveReturns(
	frame *archiveFrame, index, depth int,
) ([]archiveValue, error) {
	graph.note(frame.function)
	if depth > archiveOriginDepth || len(frame.function.returns) == 0 {
		return nil, archiveOriginError(frame.function, frame.function.node, "missing or cyclic return origin")
	}
	var values []archiveValue
	for _, statement := range frame.function.returns {
		result, err := graph.resolveReturn(frame, statement, index, depth+1)
		if err != nil {
			return nil, err
		}
		values = append(values, result...)
	}
	if len(values) == 0 {
		return nil, archiveOriginError(frame.function, frame.function.node, "no successful concrete return")
	}
	return values, nil
}

func (graph *archiveOriginGraph) resolveReturn(
	frame *archiveFrame, statement archiveReturn, index, depth int,
) ([]archiveValue, error) {
	function := frame.function
	results := statement.node.Results
	signature, ok := function.object.Type().(*types.Signature)
	if !ok {
		return nil, archiveOriginError(function, statement.node, "unresolved return signature")
	}
	if len(results) == 1 && signature.Results().Len() > 1 {
		return graph.resolve(archiveOperand{node: results[0], frame: frame, index: index}, depth+1)
	}
	if index >= len(results) {
		return nil, archiveOriginError(function, statement.node, "named or incomplete return is not proven")
	}
	if archiveNil(function.pkg.TypesInfo, results[index]) {
		if graph.failedReturn(function, statement) {
			return nil, nil
		}
		return nil, archiveOriginError(function, statement.node, "nil resource without a proven failure")
	}
	return graph.resolve(archiveOperand{node: results[index], frame: frame}, depth+1)
}

func (graph *archiveOriginGraph) failedReturn(function *archiveFunction, statement archiveReturn) bool {
	results := statement.node.Results
	signature, ok := function.object.Type().(*types.Signature)
	if !ok || len(results) != signature.Results().Len() || len(results) < 2 ||
		!types.Identical(signature.Results().At(len(results)-1).Type(), types.Universe.Lookup("error").Type()) {
		return false
	}
	last := results[len(results)-1]
	if name, ok := last.(*ast.Ident); ok {
		object := function.pkg.TypesInfo.ObjectOf(name)
		return statement.nonNil[object] && len(function.locals[object]) <= 1 && !graph.mutated[object]
	}
	call, ok := last.(*ast.CallExpr)
	if !ok {
		return false
	}
	target, ok := callObject(function.pkg.TypesInfo, call.Fun).(*types.Func)
	return ok && target.Pkg() != nil &&
		(target.Pkg().Path() == "errors" && target.Name() == "New" ||
			target.Pkg().Path() == "fmt" && target.Name() == "Errorf")
}

func (graph *archiveOriginGraph) noteOriginUse(function *archiveFunction, object types.Object, position token.Pos) {
	signature, ok := function.object.Type().(*types.Signature)
	if !ok || signature.Recv() == object {
		return // A Service receiver's ordinary business uses are not constructor escapes.
	}
	use := graph.originUses[object]
	if use == nil {
		use = &archiveOriginUse{function: function, positions: make(map[token.Pos]bool)}
		graph.originUses[object] = use
		graph.originOrder = append(graph.originOrder, object)
	}
	use.positions[position] = true
}

func (graph *archiveOriginGraph) checkOriginUses() error {
	for _, object := range graph.originOrder {
		use := graph.originUses[object]
		var failure error
		ast.Inspect(use.function.node.Body, func(node ast.Node) bool {
			if failure != nil {
				return false
			}
			name, ok := node.(*ast.Ident)
			if ok && use.function.pkg.TypesInfo.Uses[name] == object && !use.positions[name.Pos()] {
				failure = archiveOriginError(use.function, name, "constructed origin has an untracked use or escape")
			}
			return failure == nil
		})
		if failure != nil {
			return failure
		}
	}
	return nil
}

func archiveDeref(value types.Type) types.Type {
	for {
		pointer, ok := types.Unalias(value).(*types.Pointer)
		if !ok {
			return types.Unalias(value)
		}
		value = pointer.Elem()
	}
}
