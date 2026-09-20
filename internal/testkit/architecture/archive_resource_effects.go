package architecture

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"
)

func (graph *archiveOriginGraph) adapterValue(value archiveValue) error {
	if value.valueType == nil || value.isNil {
		return errArchiveOrigin
	}
	named, ok := archiveDeref(value.valueType).(*types.Named)
	if !ok || graph.contract.layer(named.Obj()) != "adapter" {
		return errArchiveOrigin
	}
	if value.frame == nil {
		return errArchiveOrigin
	}
	if value.frame.function.owner.Layer != "adapter" && len(InspectValueType(value.valueType)) != 0 {
		return archiveOriginError(value.frame.function, value.literal,
			"resource-bearing Adapter literal has no owned constructor origin")
	}
	graph.noteType(value.valueType)
	if reason := graph.archiveDependency(value.valueType, make(map[types.Type]bool)); reason != "" {
		return archiveOriginError(value.frame.function, value.literal, reason)
	}
	return nil
}

func (graph *archiveOriginGraph) resourceMethods(
	value archiveValue, contract types.Type,
) ([]ResourceLocation, error) {
	if err := graph.adapterValue(value); err != nil {
		return nil, err
	}
	resource, ok := contract.Underlying().(*types.Interface)
	if !ok || !types.Implements(value.valueType, resource) || genericResourceType(archiveDeref(value.valueType)) {
		return nil, archiveOriginError(value.frame.function, value.literal, "returned object is not the fixed resource")
	}
	methods := types.NewMethodSet(value.valueType)
	var result []ResourceLocation
	for index := range methods.Len() {
		method, ok := methods.At(index).Obj().(*types.Func)
		if !ok {
			return nil, archiveOriginError(value.frame.function, value.literal, "unresolved resource method")
		}
		if !method.Exported() {
			continue
		}
		if !slices.Contains([]string{"Next", "Read", "Complete", "Close"}, method.Name()) {
			return nil, archiveOriginError(value.frame.function, value.literal, "hidden extra resource method: "+method.Name())
		}
		function := graph.functions[method.Origin()]
		if function == nil || function.owner.Layer != "adapter" {
			return nil, archiveOriginError(value.frame.function, value.literal, "resource method has a non-Adapter origin")
		}
		if err := graph.checkArchiveEffects(function, make(map[*types.Func]bool)); err != nil {
			return nil, err
		}
		result = append(result, function.location)
	}
	if len(result) != 4 {
		return nil, archiveOriginError(value.frame.function, value.literal, "resource does not expose exactly four methods")
	}
	return result, nil
}

func (graph *archiveOriginGraph) archiveDependency(value types.Type, seen map[types.Type]bool) string {
	if value == nil {
		return "unresolved resource dependency"
	}
	if seen[value] {
		return ""
	}
	seen[value] = true
	if archiveTechnicalType(value) {
		return ""
	}
	if graph.diagnosticReporter(value) {
		graph.noteType(value)
		return ""
	}
	switch typed := value.(type) {
	case *types.Alias:
		if genericResourceType(typed) {
			return "generic resource dependency alias"
		}
		return graph.archiveDependency(types.Unalias(typed), seen)
	case *types.Named:
		return graph.archiveNamedDependency(typed, seen)
	default:
		return graph.archiveMemberDependency(value, seen)
	}
}

func (graph *archiveOriginGraph) archiveMemberDependency(value types.Type, seen map[types.Type]bool) string {
	switch typed := value.(type) {
	case *types.Pointer:
		return graph.archiveDependency(typed.Elem(), seen)
	case *types.Struct:
		return graph.archiveFieldDependencies(typed, seen)
	case *types.Array:
		return graph.archiveDependency(typed.Elem(), seen)
	case *types.Slice:
		return graph.archiveDependency(typed.Elem(), seen)
	case *types.Map:
		if reason := graph.archiveDependency(typed.Key(), seen); reason != "" {
			return reason
		}
		return graph.archiveDependency(typed.Elem(), seen)
	case *types.Chan:
		return graph.archiveDependency(typed.Elem(), seen)
	case *types.Basic:
		if typed.Kind() != types.UnsafePointer && typed.Kind() != types.Invalid {
			return ""
		}
	}
	return "resource dependency hides a callback, business interface or unresolved type"
}

func (graph *archiveOriginGraph) archiveNamedDependency(value *types.Named, seen map[types.Type]bool) string {
	for index := range value.TypeArgs().Len() {
		if reason := graph.archiveDependency(value.TypeArgs().At(index), seen); reason != "" {
			return reason
		}
	}
	object := value.Obj()
	graph.noteType(value)
	layer := graph.contract.layer(object)
	if slices.Contains([]string{"service", "repo", "transport", "bootstrap", "testkit", "tool"}, layer) {
		return "resource dependency originates in forbidden layer: " + layer
	}
	if object.Pkg() != nil && (object.Pkg().Path() == "database/sql" ||
		object.Pkg().Path() == "database/sql/driver") {
		return "resource dependency exposes SQL"
	}
	if _, contract := value.Underlying().(*types.Interface); contract {
		return "resource dependency injects an unapproved interface"
	}
	if _, callback := value.Underlying().(*types.Signature); callback {
		return "resource dependency injects a callback"
	}
	if layer == "" && object.Pkg() != nil {
		// External concrete technical state stays opaque, but generic arguments above do not.
		return ""
	}
	return graph.archiveDependency(value.Underlying(), seen)
}

func (graph *archiveOriginGraph) archiveFieldDependencies(value *types.Struct, seen map[types.Type]bool) string {
	for index := range value.NumFields() {
		field := value.Field(index)
		if graph.externalWrites[field] && len(InspectValueType(field.Type())) != 0 {
			return field.Name() + ": resource dependency field is mutated outside Adapter"
		}
		if reason := graph.archiveDependency(field.Type(), seen); reason != "" {
			return field.Name() + ": " + reason
		}
	}
	return ""
}

func archiveTechnicalType(value types.Type) bool {
	if genericResourceType(value) {
		return false
	}
	if types.Identical(value, types.Universe.Lookup("error").Type()) ||
		streamPort(value) || closeOnlyResource(value) {
		return true
	}
	named, ok := types.Unalias(value).(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != "context" {
		return false
	}
	return named.Obj().Name() == "Context" || named.Obj().Name() == "CancelFunc"
}

func (graph *archiveOriginGraph) diagnosticReporter(value types.Type) bool {
	named, ok := types.Unalias(value).(*types.Named)
	if !ok || genericResourceType(value) || graph.contract.layer(named.Obj()) != "model" ||
		named.Obj().Pkg() == nil || named.Obj().Name() != "ErrorReporter" ||
		!strings.HasSuffix(named.Obj().Pkg().Path(), "/internal/model/diagnostics") {
		return false
	}
	port, ok := named.Underlying().(*types.Interface)
	if !ok || port.NumEmbeddeds() != 0 || port.NumMethods() != 1 || port.Method(0).Name() != "Report" {
		return false
	}
	return graph.diagnosticSignature(named, port.Method(0))
}

func (graph *archiveOriginGraph) diagnosticSignature(named *types.Named, method *types.Func) bool {
	signature, ok := method.Type().(*types.Signature)
	if !ok || signature.Variadic() || signature.Params().Len() != 2 || signature.Results().Len() != 0 {
		return false
	}
	contextType := signature.Params().At(0).Type()
	event := signature.Params().At(1).Type()
	canonical := archiveFactFromPackage(named.Obj().Pkg(), "DiagnosticEvent")
	if canonical == nil || !types.Identical(event, canonical) || graph.contract.layer(canonical.Obj()) != "model" {
		return false
	}
	return archiveTechnicalType(contextType) && types.TypeString(contextType, packagePath) == "context.Context" &&
		len(InspectValueType(event)) == 0 && archiveFieldsMatch(event, map[string]types.Type{
		"Operation": types.Typ[types.String], "Code": types.Typ[types.String],
		"Message": types.Typ[types.String], "RequestID": types.Typ[types.String],
	})
}

func (graph *archiveOriginGraph) checkArchiveEffects(
	function *archiveFunction, seen map[*types.Func]bool,
) error {
	if seen[function.object] {
		return nil
	}
	seen[function.object] = true
	graph.note(function)
	if function.owner.Layer != "adapter" && function.owner.Layer != "capability" && function.owner.Layer != "model" {
		return archiveOriginError(function, function.node, "resource re-enters a business or construction layer")
	}
	var failure error
	ast.Inspect(function.node.Body, func(node ast.Node) bool {
		if failure != nil {
			return false
		}
		if name, ok := node.(*ast.Ident); ok {
			failure = graph.checkArchiveGlobal(function, name)
			return failure == nil
		}
		call, ok := node.(*ast.CallExpr)
		if !ok || function.pkg.TypesInfo.Types[call.Fun].IsType() {
			return true
		}
		failure = graph.checkArchiveCall(function, call, seen)
		return failure == nil
	})
	return failure
}

func (graph *archiveOriginGraph) checkArchiveGlobal(function *archiveFunction, name *ast.Ident) error {
	value, ok := function.pkg.TypesInfo.Uses[name].(*types.Var)
	if !ok || value.Pkg() == nil || value.Parent() != value.Pkg().Scope() {
		return nil
	}
	if types.Identical(value.Type(), types.Universe.Lookup("error").Type()) ||
		graph.diagnosticReporter(value.Type()) ||
		len(InspectValueType(value.Type())) == 0 && !archiveGlobalExecution(value.Type()) {
		return nil
	}
	if graph.external == nil {
		graph.external = newArchiveExternalProof(graph)
	}
	return graph.external.check(function, name, value)
}

func archiveContextDataCall(function *types.Func) bool {
	if function.Pkg() == nil || function.Pkg().Path() != "context" || function.Name() != "Value" {
		return false
	}
	signature, ok := function.Type().(*types.Signature)
	return ok && signature.Recv() != nil
}

func (graph *archiveOriginGraph) checkArchiveCall(
	caller *archiveFunction, call *ast.CallExpr, seen map[*types.Func]bool,
) error {
	if _, literal := call.Fun.(*ast.FuncLit); literal {
		return nil // Calls inside the locally owned literal are visited separately.
	}
	object := callObject(caller.pkg.TypesInfo, call.Fun)
	if object == nil {
		return archiveOriginError(caller, call, "unresolved executable resource dependency")
	}
	if _, builtin := object.(*types.Builtin); builtin {
		return nil
	}
	function, ok := object.(*types.Func)
	if !ok {
		if archiveTechnicalType(object.Type()) {
			return nil
		}
		return archiveOriginError(caller, call, "resource invokes an injected callback")
	}
	if archiveContextDataCall(function) {
		return archiveOriginError(caller, call, "opaque context data is not a proven owned resource source")
	}
	if sqlExecutionMethod(function) {
		return archiveOriginError(caller, call, "resource invokes SQL")
	}
	if target := graph.functions[function.Origin()]; target != nil {
		return graph.checkArchiveEffects(target, seen)
	}
	signature, ok := function.Type().(*types.Signature)
	if !ok {
		return archiveOriginError(caller, call, "unresolved callable signature")
	}
	if signature.Recv() != nil && (archiveTechnicalType(signature.Recv().Type()) ||
		graph.diagnosticReporter(signature.Recv().Type())) {
		return nil
	}
	if graph.contract.layer(function) != "" {
		return archiveOriginError(caller, call, "resource invokes an unbound local or business method")
	}
	return nil
}
