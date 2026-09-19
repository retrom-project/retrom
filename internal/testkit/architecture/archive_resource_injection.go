package architecture

import "go/types"

// Bootstrap may inject the fixed Reporter and context, or ordinary closed values.
// Reader/Closer-shaped business objects cannot be laundered into the resource factory.
// Streams opened by Adapter itself remain implementation state, not injected ports.
func (graph *archiveOriginGraph) externalArchiveInput(value types.Type, seen map[types.Type]bool) bool {
	if value == nil {
		return true
	}
	if seen[value] {
		return false
	}
	seen[value] = true
	if graph.diagnosticReporter(value) {
		return false
	}
	if named, ok := types.Unalias(value).(*types.Named); ok && named.Obj().Pkg() != nil &&
		named.Obj().Pkg().Path() == "context" && named.Obj().Name() == "Context" {
		return false
	}
	if len(InspectValueType(value)) == 0 {
		return false
	}
	return graph.externalArchiveMembers(value, seen)
}

func (graph *archiveOriginGraph) externalArchiveMembers(value types.Type, seen map[types.Type]bool) bool {
	switch typed := value.(type) {
	case *types.Alias:
		return graph.externalArchiveInput(types.Unalias(typed), seen)
	case *types.Named:
		return graph.externalArchiveInput(typed.Underlying(), seen)
	case *types.Pointer:
		return graph.externalArchiveInput(typed.Elem(), seen)
	case *types.Struct:
		for index := range typed.NumFields() {
			if graph.externalArchiveInput(typed.Field(index).Type(), seen) {
				return true
			}
		}
		return false
	case *types.Slice:
		return graph.externalArchiveInput(typed.Elem(), seen)
	case *types.Array:
		return graph.externalArchiveInput(typed.Elem(), seen)
	case *types.Map:
		return graph.externalArchiveInput(typed.Key(), seen) || graph.externalArchiveInput(typed.Elem(), seen)
	default:
		return true
	}
}
