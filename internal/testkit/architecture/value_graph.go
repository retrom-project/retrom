package architecture

import (
	"go/types"
	"strings"
)

// TypeIssue records the complete path to a non-value member of a contract.
type TypeIssue struct {
	Kind string
	Path []string
}

type valueGraph struct {
	seen   map[types.Type]bool
	issues []TypeIssue
}

// InspectValueType follows actual types, including aliases and instantiated generics.
// The caller, not the value walker, owns narrow exceptions at port argument positions.
func InspectValueType(value types.Type) []TypeIssue {
	graph := valueGraph{seen: make(map[types.Type]bool)}
	graph.visit(value, []string{types.TypeString(value, packagePath)})
	return graph.issues
}

func packagePath(pkg *types.Package) string { return pkg.Path() }

func (graph *valueGraph) reject(kind string, chain []string) {
	graph.issues = append(graph.issues, TypeIssue{Kind: kind, Path: append([]string(nil), chain...)})
}

func (graph *valueGraph) visit(value types.Type, chain []string) {
	if graph.seen[value] {
		return
	}
	graph.seen[value] = true
	switch typed := value.(type) {
	case *types.Alias:
		graph.visit(types.Unalias(typed), append(chain, "alias"))
	case *types.Named:
		graph.visitNamed(typed, chain)
	case *types.Pointer:
		graph.visit(typed.Elem(), append(chain, "*"))
	case *types.Array:
		graph.visit(typed.Elem(), append(chain, "array element"))
	case *types.Slice:
		graph.visit(typed.Elem(), append(chain, "slice element"))
	case *types.Map:
		graph.visit(typed.Key(), append(chain, "map key"))
		graph.visit(typed.Elem(), append(chain, "map value"))
	case *types.Struct:
		graph.visitFields(typed, chain)
	default:
		graph.visitLeaf(value, chain)
	}
}

func (graph *valueGraph) visitNamed(value *types.Named, chain []string) {
	object := value.Obj()
	if object.Pkg() != nil {
		name := object.Pkg().Path()
		switch {
		case name == "database/sql" || name == "database/sql/driver" ||
			strings.HasSuffix(name, "/internal/repo/dbexec"):
			graph.reject("SQL capability or nullable value", chain)
			return
		case name == "context", name == "time" && object.Name() == "Time":
			graph.reject("context or implicit clock value", chain)
			return
		case name == "os", name == "net", name == "net/http":
			graph.reject("resource or protocol handle", chain)
			return
		}
	}
	graph.visit(value.Underlying(), append(chain, object.Name()))
}

func (graph *valueGraph) visitFields(value *types.Struct, chain []string) {
	for index := range value.NumFields() {
		field := value.Field(index)
		graph.visit(field.Type(), append(chain, "field "+field.Name()))
	}
}

func (graph *valueGraph) visitLeaf(value types.Type, chain []string) {
	switch typed := value.(type) {
	case *types.Signature:
		graph.reject("executable callback", chain)
	case *types.Chan:
		graph.reject("channel", chain)
	case *types.Interface:
		if typed.Complete().NumMethods() == 0 {
			graph.reject("open interface value", chain)
		} else {
			graph.reject("capability interface", chain)
		}
	case *types.TypeParam, *types.Union:
		graph.reject("unresolved type parameter", chain)
	case *types.Basic:
		if typed.Kind() == types.UnsafePointer || typed.Kind() == types.Invalid {
			graph.reject("unsafe or unresolved value", chain)
		}
	default:
		graph.reject("unsupported value type", chain)
	}
}
