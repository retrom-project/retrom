package architecture

import "slices"

// CompiledSources includes generated and otherwise ignored files reachable by Go.
func CompiledSources(graph []GoPackageInventory) []string {
	result := make([]string, 0)
	for _, pkg := range graph {
		result = append(result, pkg.Files...)
	}
	slices.Sort(result)
	return slices.Compact(result)
}

// ValidateCompilationScope rejects production dependencies hidden from Git discovery.
func ValidateCompilationScope(sources []string, graph []GoPackageInventory) []Violation {
	result := make([]Violation, 0)
	generated := []string{
		"internal/transport/httpapi/generated/models.gen.go",
		"internal/transport/httpapi/generated/server.gen.go",
		"internal/transport/httpapi/generated/spec.gen.go",
	}
	for _, name := range CompiledSources(graph) {
		if !slices.Contains(sources, name) && !slices.Contains(generated, name) {
			result = append(result, Violation{
				Rule: "AR01", File: name, Line: 1, DependencyChain: []string{name},
				Message: "compiled source is absent from tracked and nonignored sources",
			})
		}
	}
	return result
}
