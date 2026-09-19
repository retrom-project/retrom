package architecture

import (
	"go/types"
	"slices"
	"testing"
)

type rf03TypeOrigins struct {
	seen     map[types.Type]bool
	file     string
	line     int
	symbol   string
	findings []Violation
}

func rf03DefinitionOrigins(value types.Type, file string, line int, symbol string) []Violation {
	walk := rf03TypeOrigins{seen: make(map[types.Type]bool), file: file, line: line, symbol: symbol}
	walk.visit(value, []string{symbol})
	return walk.findings
}

func (walk *rf03TypeOrigins) visit(value types.Type, chain []string) {
	if value == nil || walk.seen[value] {
		return
	}
	walk.seen[value] = true
	switch item := value.(type) {
	case *types.Alias:
		walk.definition(item.Obj(), chain)
		walk.visit(item.Rhs(), append(slices.Clone(chain), item.Obj().Name()))
	case *types.Named:
		walk.named(item, chain)
	case *types.Struct:
		for index := range item.NumFields() {
			field := item.Field(index)
			walk.visit(field.Type(), append(slices.Clone(chain), field.Name()))
		}
	case *types.Signature:
		walk.signature(item, chain)
	case *types.Interface:
		walk.declaredInterface(item, chain)
	case *types.Map:
		walk.visit(item.Key(), append(slices.Clone(chain), "map key"))
		walk.visit(item.Elem(), append(slices.Clone(chain), "map value"))
	case *types.Tuple:
		walk.tuple(item, chain)
	case *types.Union:
		for index := range item.Len() {
			walk.visit(item.Term(index).Type(), chain)
		}
	case *types.TypeParam:
		walk.visit(item.Constraint(), chain)
	case interface{ Elem() types.Type }:
		walk.visit(item.Elem(), chain)
	}
}

func (walk *rf03TypeOrigins) tuple(value *types.Tuple, chain []string) {
	for index := range value.Len() {
		walk.visit(value.At(index).Type(), chain)
	}
}

func (walk *rf03TypeOrigins) named(value *types.Named, chain []string) {
	object := value.Obj()
	if object.Pkg() == nil {
		return // Builtins such as error have no repository definition origin.
	}
	walk.definition(object, chain)
	next := append(slices.Clone(chain), inventoryObjectID(object))
	for index := range value.TypeArgs().Len() {
		walk.visit(value.TypeArgs().At(index), next)
	}
	for index := range value.TypeParams().Len() {
		walk.visit(value.TypeParams().At(index), next)
	}
	// External packages cannot depend on Retrom's internal definitions. Their
	// generic arguments above still belong to the caller and must be followed.
	if object.Pkg() == nil || !inPackageTree(object.Pkg().Path(), "retrom") {
		return
	}
	walk.visit(value.Underlying(), next)
	for index := range value.NumMethods() {
		method := value.Method(index)
		if method.Exported() {
			walk.visit(method.Type(), append(slices.Clone(next), method.Name()))
		}
	}
}

func (walk *rf03TypeOrigins) signature(value *types.Signature, chain []string) {
	walk.visit(value.Params(), append(slices.Clone(chain), "parameters"))
	walk.visit(value.Results(), append(slices.Clone(chain), "results"))
	for index := range value.TypeParams().Len() {
		walk.visit(value.TypeParams().At(index), chain)
	}
}

func (walk *rf03TypeOrigins) declaredInterface(value *types.Interface, chain []string) {
	for index := range value.NumEmbeddeds() {
		walk.visit(value.EmbeddedType(index), chain)
	}
	for index := range value.NumExplicitMethods() {
		method := value.ExplicitMethod(index)
		walk.visit(method.Type(), append(slices.Clone(chain), method.Name()))
	}
}

func (walk *rf03TypeOrigins) definition(object *types.TypeName, chain []string) {
	if object.Pkg() == nil {
		return
	}
	for _, layer := range []string{"adapter", "transport", "service", "repo"} {
		if inPackageTree(object.Pkg().Path(), "retrom/internal/"+layer) {
			walk.findings = append(walk.findings, Violation{
				Rule: "AR03", File: walk.file, Line: walk.line, Symbol: walk.symbol,
				DependencyChain: append(slices.Clone(chain), inventoryObjectID(object)),
				Message:         "Model exposed type reaches a concrete " + layer + " definition",
			})
		}
	}
}

func rf03AssertClockPortDeclaration(t *testing.T, repository *rf03Repository) {
	t.Helper()
	found := false
	for _, graph := range repository.graphs {
		for _, pkg := range graph {
			if pkg.ID != pkg.PkgPath || pkg.PkgPath != "retrom/internal/model/clock" {
				continue
			}
			object := pkg.Types.Scope().Lookup("Clock")
			if object == nil {
				t.Fatal("real Model Clock declaration is missing")
			}
			port, ok := object.Type().Underlying().(*types.Interface)
			if !ok || port.NumMethods() != 1 || port.Method(0).Name() != "NowMS" {
				t.Fatalf("Clock is not the declared technical port: %s", object.Type())
			}
			issues := rf03DefinitionOrigins(object.Type(), "internal/model/clock/clock.go", 1, "clock.Clock")
			if len(issues) != 0 {
				t.Fatalf("a declared external port was treated as its implementation: %+v", issues)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("current type graph does not contain Model Clock")
	}
}

func TestRF03DefinitionOriginsFollowAliasesAndGenerics(t *testing.T) {
	t.Parallel()
	model := types.NewPackage("retrom/internal/model/example", "example")
	adapter := types.NewPackage("retrom/internal/adapter/files/source", "source")
	handle := types.NewNamed(types.NewTypeName(0, adapter, "Handle", nil), types.NewStruct(nil, nil), nil)
	parameter := types.NewTypeParam(types.NewTypeName(0, model, "T", nil),
		types.NewInterfaceType(nil, nil).Complete())
	box := types.NewNamed(types.NewTypeName(0, model, "Box", nil),
		types.NewStruct([]*types.Var{types.NewField(0, model, "Value", parameter, false)}, nil), nil)
	box.SetTypeParams([]*types.TypeParam{parameter})
	instantiated, err := types.Instantiate(nil, box, []types.Type{types.NewPointer(handle)}, true)
	if err != nil {
		t.Fatal(err)
	}
	alias := types.NewAlias(types.NewTypeName(0, model, "Alias", nil), instantiated)
	nested := types.NewStruct([]*types.Var{
		types.NewField(0, model, "Values", types.NewSlice(types.NewMap(types.Typ[types.String], alias)), false),
	}, nil)
	issues := rf03DefinitionOrigins(nested, "model.go", 1, "example.Value")
	if len(issues) != 1 || !slices.Contains(issues[0].DependencyChain, adapter.Path()+".Handle") {
		t.Fatalf("generic alias hid an implementation definition: %+v", issues)
	}
	recursive := types.NewNamed(types.NewTypeName(0, model, "Recursive", nil), types.NewStruct(nil, nil), nil)
	recursive.SetUnderlying(types.NewStruct([]*types.Var{
		types.NewField(0, model, "Next", types.NewPointer(recursive), false),
		types.NewField(0, model, "Err", types.Universe.Lookup("error").Type(), false),
	}, nil))
	if issues := rf03DefinitionOrigins(recursive, "model.go", 2, "example.Recursive"); len(issues) != 0 {
		t.Fatalf("recursive pure value rejected: %+v", issues)
	}
	result := types.NewTuple(types.NewVar(0, nil, "", types.Typ[types.Int64]))
	port := types.NewInterfaceType([]*types.Func{
		types.NewFunc(0, model, "NowMS", types.NewSignatureType(nil, nil, nil, nil, result, false)),
	}, nil).Complete()
	handle.AddMethod(types.NewFunc(0, adapter, "NowMS",
		types.NewSignatureType(types.NewVar(0, adapter, "receiver", types.NewPointer(handle)),
			nil, nil, nil, result, false)))
	if !types.Implements(types.NewPointer(handle), port) {
		t.Fatal("test adapter does not implement the declared port")
	}
	if issues := rf03DefinitionOrigins(port, "model.go", 3, "example.Clock"); len(issues) != 0 {
		t.Fatalf("port declaration was confused with an external implementation: %+v", issues)
	}
}
