package architecture

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
)

type archiveExternalCall struct {
	target *archiveExternalFrame
}

type archiveExternalCallee struct {
	function  *types.Func
	signature *types.Signature
	receiver  *archiveExternalValue
}

func (proof *archiveExternalProof) bindCall(
	frame *archiveExternalFrame, call *ast.CallExpr, depth int,
) (archiveExternalCall, error) {
	if depth > archiveOriginDepth {
		return archiveExternalCall{}, fmt.Errorf("%w: external call depth", errArchiveOrigin)
	}
	info := frame.function.pkg.TypesInfo
	object := callObject(info, call.Fun)
	_, builtin := object.(*types.Builtin)
	if info.Types[call.Fun].IsType() || builtin {
		return archiveExternalCall{}, nil
	}
	callee, err := proof.resolveCallee(frame, call, object, depth)
	if err != nil {
		return archiveExternalCall{}, err
	}
	if archiveExternalResourceSignature(callee.signature) {
		return archiveExternalCall{}, fmt.Errorf("%w: external global dispatch exposes a resource", errArchiveOrigin)
	}
	pkg := proof.packages[callee.function.Pkg()]
	if pkg != nil && pkg.Module == nil {
		if !archiveExternalPureParameters(callee.signature) {
			return archiveExternalCall{}, fmt.Errorf("%w: opaque standard-library executable argument", errArchiveOrigin)
		}
		return archiveExternalCall{}, nil
	}
	target, err := proof.declaration(callee.function)
	if err != nil {
		return archiveExternalCall{}, err
	}
	next := &archiveExternalFrame{function: target, bindings: make(map[types.Object]*archiveExternalValue)}
	if callee.signature.Recv() != nil {
		next.bindings[callee.signature.Recv()] = callee.receiver
	}
	for index := range callee.signature.Params().Len() {
		parameter := callee.signature.Params().At(index)
		if archiveExternalPure(parameter.Type()) {
			continue
		}
		value, err := proof.value(frame, call.Args[index], depth+1, false)
		if err != nil {
			return archiveExternalCall{}, err
		}
		next.bindings[parameter] = value
	}
	return archiveExternalCall{target: next}, nil
}

func (proof *archiveExternalProof) resolveCallee(
	frame *archiveExternalFrame, call *ast.CallExpr, object types.Object, depth int,
) (archiveExternalCallee, error) {
	function, ok := object.(*types.Func)
	if !ok {
		return archiveExternalCallee{}, fmt.Errorf("%w: unknown external executable dispatch", errArchiveOrigin)
	}
	signature, _ := function.Type().(*types.Signature)
	if !archiveExternalInvocation(signature) || len(call.Args) != signature.Params().Len() {
		return archiveExternalCallee{}, fmt.Errorf("%w: unsupported external invocation shape", errArchiveOrigin)
	}
	result := archiveExternalCallee{function: function, signature: signature}
	if signature.Recv() == nil {
		return result, nil
	}
	info := frame.function.pkg.TypesInfo
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || info.Selections[selector] == nil || info.Selections[selector].Kind() != types.MethodVal {
		return archiveExternalCallee{}, fmt.Errorf("%w: external receiver is not direct", errArchiveOrigin)
	}
	receiver, err := proof.value(frame, selector.X, depth+1, false)
	if err != nil {
		return archiveExternalCallee{}, err
	}
	function, receiver, err = proof.method(receiver, function.Name(), depth+1)
	if err != nil {
		return archiveExternalCallee{}, err
	}
	signature, _ = function.Type().(*types.Signature)
	if !archiveExternalInvocation(signature) {
		return archiveExternalCallee{}, fmt.Errorf("%w: generic or variadic external implementation", errArchiveOrigin)
	}
	return archiveExternalCallee{function: function, signature: signature, receiver: receiver}, nil
}

func archiveExternalInvocation(signature *types.Signature) bool {
	return signature != nil && !signature.Variadic() &&
		signature.TypeParams().Len() == 0 && signature.RecvTypeParams().Len() == 0
}

func archiveExternalPureParameters(signature *types.Signature) bool {
	for index := range signature.Params().Len() {
		if !archiveExternalPure(signature.Params().At(index).Type()) {
			return false
		}
	}
	return true
}

func archiveExternalResourceSignature(signature *types.Signature) bool {
	for _, tuple := range []*types.Tuple{signature.Params(), signature.Results()} {
		for index := range tuple.Len() {
			value := tuple.At(index).Type()
			if streamPort(value) || closeOnlyResource(value) || archiveReaderCandidate(value) {
				return true
			}
		}
	}
	return false
}

func (proof *archiveExternalProof) method(
	receiver *archiveExternalValue, name string, depth int,
) (*types.Func, *archiveExternalValue, error) {
	if depth > archiveOriginDepth || receiver == nil {
		return nil, nil, fmt.Errorf("%w: unresolved external receiver", errArchiveOrigin)
	}
	if archiveGlobalStream(receiver.valueType) || streamPort(receiver.valueType) ||
		closeOnlyResource(receiver.valueType) || archiveReaderCandidate(receiver.valueType) {
		return nil, nil, fmt.Errorf("%w: global stream or resource factory", errArchiveOrigin)
	}
	method := types.NewMethodSet(receiver.valueType).Lookup(nil, name)
	if method == nil {
		method = types.NewMethodSet(types.NewPointer(receiver.valueType)).Lookup(nil, name)
	}
	if method == nil {
		return nil, nil, fmt.Errorf("%w: concrete external method missing", errArchiveOrigin)
	}
	indexes := method.Index()
	if len(indexes) > 1 {
		nested, err := proof.fieldPath(receiver, indexes[:len(indexes)-1])
		if err != nil {
			return nil, nil, err
		}
		return proof.method(nested, name, depth+1)
	}
	function, ok := method.Obj().(*types.Func)
	if !ok {
		return nil, nil, fmt.Errorf("%w: unresolved external method object", errArchiveOrigin)
	}
	if _, ok := archiveDeref(receiver.valueType).Underlying().(*types.Interface); ok {
		return nil, nil, fmt.Errorf("%w: unknown external interface dispatch", errArchiveOrigin)
	}
	return function, receiver, nil
}

func (proof *archiveExternalProof) checkFrame(frame *archiveExternalFrame, depth int) error {
	if depth > archiveOriginDepth || proof.active[frame.function.object] {
		return fmt.Errorf("%w: cyclic external method effects", errArchiveOrigin)
	}
	key := archiveExternalFrameKey(frame)
	if proof.checked[key] {
		return nil
	}
	proof.active[frame.function.object] = true
	defer delete(proof.active, frame.function.object)
	var failure error
	ast.Inspect(frame.function.node.Body, func(node ast.Node) bool {
		if failure != nil {
			return false
		}
		failure = proof.checkNode(frame, node, depth)
		return failure == nil
	})
	if failure == nil {
		proof.checked[key] = true
	}
	return failure
}

func (proof *archiveExternalProof) checkNode(frame *archiveExternalFrame, node ast.Node, depth int) error {
	switch value := node.(type) {
	case *ast.FuncLit:
		return fmt.Errorf("%w: external closure effects unresolved", errArchiveOrigin)
	case *ast.AssignStmt:
		return archiveExternalWrite(frame, value)
	case *ast.RangeStmt:
		return archiveExternalRangeWrite(frame, value)
	case *ast.UnaryExpr:
		_, literal := value.X.(*ast.CompositeLit)
		if value.Op == token.AND && !literal && !archiveExternalPure(frame.function.pkg.TypesInfo.TypeOf(value.X)) {
			return fmt.Errorf("%w: external executable address escaped", errArchiveOrigin)
		}
	case *ast.Ident:
		return proof.externalGlobalUse(frame, value, depth+1)
	case *ast.CallExpr:
		next, err := proof.bindCall(frame, value, depth+1)
		if err != nil {
			return err
		}
		if next.target != nil {
			return proof.checkFrame(next.target, depth+1)
		}
	}
	return nil
}

func (proof *archiveExternalProof) externalGlobalUse(frame *archiveExternalFrame, name *ast.Ident, depth int) error {
	global, ok := frame.function.pkg.TypesInfo.Uses[name].(*types.Var)
	if !ok || global.Pkg() == nil || global.Parent() != global.Pkg().Scope() {
		return nil
	}
	if types.Identical(global.Type(), types.Universe.Lookup("error").Type()) ||
		archiveExternalPure(global.Type()) && !archiveGlobalExecution(global.Type()) {
		pkg := proof.packages[global.Pkg()]
		if pkg != nil && pkg.Module != nil {
			return proof.noteExternal(pkg, global.Pos())
		}
		return nil
	}
	if _, err := proof.global(global, depth+1); err != nil {
		return err
	}
	return proof.auditGlobal(global, make(map[*types.Var]bool))
}

func archiveExternalFrameKey(frame *archiveExternalFrame) string {
	parts := make([]string, 0, len(frame.bindings))
	for object, value := range frame.bindings {
		parts = append(parts, object.Name()+"="+archiveExternalValueKey(value))
	}
	slices.Sort(parts)
	return inventoryObjectID(frame.function.object) + "(" + strings.Join(parts, ",") + ")"
}

func archiveExternalValueKey(value *archiveExternalValue) string {
	if value == nil {
		return "?"
	}
	parts := make([]string, 0, len(value.fields))
	for field, member := range value.fields {
		parts = append(parts, field.Name()+"="+archiveExternalValueKey(member))
	}
	slices.Sort(parts)
	return types.TypeString(value.valueType, packagePath) + "{" + strings.Join(parts, ",") + "}"
}

func archiveExternalWrite(frame *archiveExternalFrame, statement *ast.AssignStmt) error {
	info := frame.function.pkg.TypesInfo
	for _, left := range statement.Lhs {
		if name, ok := left.(*ast.Ident); ok {
			object := info.ObjectOf(name)
			if frame.bindings[object] == nil {
				continue
			}
		}
		if !archiveExternalPure(info.TypeOf(left)) {
			return fmt.Errorf("%w: external executable state is reassigned", errArchiveOrigin)
		}
	}
	return nil
}

func archiveExternalRangeWrite(frame *archiveExternalFrame, statement *ast.RangeStmt) error {
	info := frame.function.pkg.TypesInfo
	if !archiveExternalPure(info.TypeOf(statement.X)) {
		return fmt.Errorf("%w: external range source has executable or unresolved values", errArchiveOrigin)
	}
	for _, target := range []ast.Expr{statement.Key, statement.Value} {
		if target == nil {
			continue
		}
		if err := archiveExternalRangeTarget(info, statement.Tok, target); err != nil {
			return err
		}
	}
	return nil
}

func archiveExternalRangeTarget(info *types.Info, operation token.Token, expression ast.Expr) error {
	name, ok := expression.(*ast.Ident)
	if !ok {
		return fmt.Errorf("%w: unsupported external range write target", errArchiveOrigin)
	}
	if name.Name == "_" {
		return nil
	}
	if operation != token.DEFINE && operation != token.ASSIGN {
		return fmt.Errorf("%w: unsupported external range assignment", errArchiveOrigin)
	}
	object := info.Uses[name]
	if operation == token.DEFINE {
		object = info.Defs[name]
	}
	variable, ok := object.(*types.Var)
	if !ok || !archiveExternalPure(variable.Type()) {
		return fmt.Errorf("%w: external range writes executable or unresolved state", errArchiveOrigin)
	}
	return nil
}
