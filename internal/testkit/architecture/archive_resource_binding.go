package architecture

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"
)

type archiveResourceAnalysis struct {
	contract archiveContract
	origins  *archiveOriginGraph
}

func (analysis *archiveResourceAnalysis) prove(method *types.Func, value types.Type) ArchiveResourceProof {
	proof := ArchiveResourceProof{
		Resource: types.TypeString(value, packagePath), Status: "WRONG_SHAPE",
		Bindings: []ArchiveResourceBinding{}, Sources: []SourceFile{},
	}
	if proof.Reason = analysis.contract.validate(value); proof.Reason != "" {
		return proof
	}
	if analysis.origins == nil {
		analysis.origins = newArchiveOriginGraph(analysis.contract)
	}
	origins := analysis.origins
	origins.sources = make(map[string]bool)
	origins.originUses = make(map[types.Object]*archiveOriginUse)
	origins.originOrder = nil
	facts := []types.Type{
		analysis.contract.header, analysis.contract.entry, analysis.contract.content, analysis.contract.nested, value,
	}
	for _, fact := range facts {
		origins.noteType(fact)
	}
	proof.Status = "UNBOUND"
	calls := origins.callers[method.Origin()]
	if len(calls) == 0 {
		proof.Reason = "no actual production acquisition through the Model port"
		return proof
	}
	for _, call := range calls {
		bindings, err := origins.proveConsumer(call, value)
		if err != nil {
			proof.Status, proof.Reason = "UNPROVEN", err.Error()
			return proof
		}
		proof.Bindings = append(proof.Bindings, bindings...)
	}
	if err := origins.checkOriginUses(); err != nil {
		proof.Status, proof.Reason = "UNPROVEN", err.Error()
		return proof
	}
	sources, err := origins.snapshot()
	if err != nil {
		proof.Status, proof.Reason = "UNPROVEN", err.Error()
		return proof
	}
	if origins.external != nil {
		external, err := origins.external.snapshot()
		if err != nil {
			proof.Status, proof.Reason = "UNPROVEN", err.Error()
			return proof
		}
		proof.ExternalSources = external
	}
	proof.Sources = sources
	proof.Status = "PROVEN"
	return proof
}

func (graph *archiveOriginGraph) noteType(value types.Type) {
	named, ok := archiveDeref(value).(*types.Named)
	if !ok {
		return
	}
	for _, pkg := range graph.contract.graph {
		if pkg.ID == pkg.PkgPath && pkg.Types == named.Obj().Pkg() {
			location := graph.location(pkg, named.Obj().Pos(), named.Obj().Name())
			graph.sources[location.File] = true
		}
	}
}

func (graph *archiveOriginGraph) proveConsumer(
	call archiveCall, resource types.Type,
) ([]ArchiveResourceBinding, error) {
	signature, ok := call.caller.object.Type().(*types.Signature)
	if !ok || call.caller.owner.Layer != "service" || signature.Recv() == nil {
		return nil, archiveOriginError(call.caller, call.node, "acquisition needs a constructed Service receiver")
	}
	selector, ok := call.node.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, archiveOriginError(call.caller, call.node, "indirect acquisition method value")
	}
	constructions := graph.serviceConstructions(signature.Recv().Type())
	if len(constructions) == 0 {
		return nil, archiveOriginError(call.caller, call.node, "no reachable Bootstrap injection")
	}
	var result []ArchiveResourceBinding
	for _, construction := range constructions {
		bindings, err := graph.proveConstruction(call, selector.X, construction, resource)
		if err != nil {
			return nil, err
		}
		result = append(result, bindings...)
	}
	if len(result) == 0 {
		return nil, archiveOriginError(call.caller, call.node, "no resolved Bootstrap injection")
	}
	return result, nil
}

func (graph *archiveOriginGraph) serviceConstructions(receiver types.Type) []archiveCall {
	var result []archiveCall
	for target, calls := range graph.callers {
		function := graph.functions[target]
		if function == nil || function.owner.Layer != "service" {
			continue
		}
		signature, ok := target.Type().(*types.Signature)
		if !ok || signature.Results().Len() == 0 ||
			!types.Identical(archiveDeref(signature.Results().At(0).Type()), archiveDeref(receiver)) {
			continue
		}
		for _, call := range calls {
			if graph.reachable[call.caller.object] &&
				call.caller.owner.Layer != "service" {
				result = append(result, call)
			}
		}
	}
	slices.SortFunc(result, func(left, right archiveCall) int {
		return strings.Compare(left.caller.location.File, right.caller.location.File) + compareArchiveCallLine(left, right)
	})
	return result
}

func compareArchiveCallLine(left, right archiveCall) int {
	if left.caller.location.File == right.caller.location.File {
		return left.caller.pkg.Fset.Position(left.node.Pos()).Line - right.caller.pkg.Fset.Position(right.node.Pos()).Line
	}
	return 0
}

func (graph *archiveOriginGraph) proveConstruction(
	acquire archiveCall, receiver ast.Expr, construction archiveCall, resource types.Type,
) ([]ArchiveResourceBinding, error) {
	if construction.caller.owner.Layer != "bootstrap" {
		return nil, archiveOriginError(construction.caller, construction.node, "resource graph constructed outside Bootstrap")
	}
	frames, err := graph.reachableFrames(construction.caller.object, nil)
	if err != nil {
		return nil, err
	}
	var result []ArchiveResourceBinding
	for _, frame := range frames {
		services, err := graph.resolve(archiveOperand{node: construction.node, frame: frame}, 0)
		if err != nil {
			return nil, err
		}
		for _, service := range services {
			bindings, err := graph.proveFactory(acquire, receiver, construction, service, resource)
			if err != nil {
				return nil, err
			}
			result = append(result, bindings...)
		}
	}
	return result, nil
}

func (graph *archiveOriginGraph) reachableFrames(
	object *types.Func, visiting map[*types.Func]bool,
) ([]*archiveFrame, error) {
	function := graph.functions[object]
	if function == nil {
		return nil, errArchiveOrigin
	}
	graph.note(function)
	if len(visiting) >= archiveOriginDepth || visiting[object] {
		return nil, archiveOriginError(function, function.node, "cyclic or over-limit Bootstrap call path")
	}
	if graph.roots[object] {
		return []*archiveFrame{{function: function, bindings: make(map[types.Object]archiveOperand)}}, nil
	}
	next := make(map[*types.Func]bool, len(visiting)+1)
	for key, value := range visiting {
		next[key] = value
	}
	next[object] = true
	var result []*archiveFrame
	for _, call := range graph.callers[object] {
		if !graph.reachable[call.caller.object] {
			continue
		}
		parents, err := graph.reachableFrames(call.caller.object, next)
		if err != nil {
			return nil, err
		}
		for _, parent := range parents {
			frame, err := graph.bridgeFrame(call, parent)
			if err != nil {
				return nil, err
			}
			result = append(result, frame)
		}
		if len(result) > 64 {
			return nil, archiveOriginError(function, function.node, "more than 64 Bootstrap call paths")
		}
	}
	return result, nil
}

func (graph *archiveOriginGraph) proveFactory(
	acquire archiveCall, receiver ast.Expr, construction archiveCall, service archiveValue, resource types.Type,
) ([]ArchiveResourceBinding, error) {
	signature, ok := acquire.caller.object.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return nil, archiveOriginError(acquire.caller, acquire.node, "missing Service receiver signature")
	}
	frame := &archiveFrame{
		function: acquire.caller,
		bindings: map[types.Object]archiveOperand{signature.Recv(): {values: []archiveValue{service}}},
	}
	factories, err := graph.resolve(archiveOperand{node: receiver, frame: frame}, 0)
	if err != nil {
		return nil, err
	}
	var result []ArchiveResourceBinding
	for _, factory := range factories {
		bindings, err := graph.proveFactoryReturn(acquire, construction, frame, factory, resource)
		if err != nil {
			return nil, err
		}
		result = append(result, bindings...)
	}
	return result, nil
}

func (graph *archiveOriginGraph) proveFactoryReturn(
	acquire, construction archiveCall, consumerFrame *archiveFrame, factory archiveValue, resource types.Type,
) ([]ArchiveResourceBinding, error) {
	if err := graph.adapterValue(factory); err != nil {
		return nil, err
	}
	object, _, _ := types.LookupFieldOrMethod(factory.valueType, true, acquire.target.Pkg(), acquire.target.Name())
	method, ok := object.(*types.Func)
	if !ok || graph.contract.layer(method) != "adapter" {
		return nil, archiveOriginError(acquire.caller, acquire.node, "factory method is not Adapter-owned")
	}
	function := graph.functions[method.Origin()]
	if function == nil {
		return nil, archiveOriginError(acquire.caller, acquire.node, "factory has no production body")
	}
	signature, ok := method.Type().(*types.Signature)
	if !ok || signature.Recv() == nil || signature.Params().Len() != len(acquire.node.Args) {
		return nil, archiveOriginError(acquire.caller, acquire.node, "unresolved factory signature")
	}
	frame := &archiveFrame{function: function, bindings: make(map[types.Object]archiveOperand)}
	frame.bindings[signature.Recv()] = archiveOperand{values: []archiveValue{factory}}
	for index, argument := range acquire.node.Args {
		frame.bindings[signature.Params().At(index)] = archiveOperand{node: argument, frame: consumerFrame}
	}
	values, err := graph.resolveReturns(frame, 0, 0)
	if err != nil {
		return nil, err
	}
	if err := graph.checkArchiveEffects(function, make(map[*types.Func]bool)); err != nil {
		return nil, err
	}
	return graph.proveResources(acquire, construction, function, values, resource)
}

func (graph *archiveOriginGraph) proveResources(
	acquire, construction archiveCall, factory *archiveFunction, values []archiveValue, resource types.Type,
) ([]ArchiveResourceBinding, error) {
	result := make([]ArchiveResourceBinding, 0, len(values))
	for _, value := range values {
		methods, err := graph.resourceMethods(value, resource)
		if err != nil {
			return nil, err
		}
		result = append(result, ArchiveResourceBinding{
			Consumer: graph.location(acquire.caller.pkg, acquire.node.Pos(), inventoryObjectID(acquire.caller.object)),
			Injection: graph.location(
				construction.caller.pkg, construction.node.Pos(), inventoryObjectID(construction.caller.object),
			),
			Factory: factory.location, Return: value.location,
			Concrete: types.TypeString(value.valueType, packagePath), Methods: methods,
		})
	}
	return result, nil
}
