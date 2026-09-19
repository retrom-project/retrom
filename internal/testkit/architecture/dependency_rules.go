package architecture

import (
	"path"
	"slices"
	"strings"
)

// InspectDependencyRules checks every production syntax edge, including inactive files.
// It reports a resolved import chain for direct and transitive layer pollution.
func InspectDependencyRules(sources []GoSourceInventory, registry OwnershipRegistry) []Violation {
	owners := make(map[string]PackageOwnership)
	edges := make(map[string][]string)
	for _, entry := range registry.Packages {
		owners["retrom/"+entry.Path] = entry
	}
	for _, source := range sources {
		if strings.HasSuffix(source.File, "_test.go") {
			continue
		}
		key := "retrom/" + path.Dir(source.File)
		edges[key] = append(edges[key], source.Imports...)
	}
	violations := make([]Violation, 0)
	for _, source := range sources {
		if strings.HasSuffix(source.File, "_test.go") {
			continue
		}
		key := "retrom/" + path.Dir(source.File)
		owner := owners[key]
		if slices.Contains([]string{"testkit", "tool", "fixture"}, owner.Layer) {
			continue
		}
		violations = append(violations, inspectImportChains(source, key, owners, edges)...)
	}
	slices.SortFunc(violations, compareViolations)
	return slices.CompactFunc(violations, equalViolation)
}

func inspectImportChains(
	source GoSourceInventory, key string, owners map[string]PackageOwnership, edges map[string][]string,
) []Violation {
	violations := make([]Violation, 0)
	for _, dependency := range source.Imports {
		chain, rule, reason := forbiddenImportChain(owners[key], []string{key, dependency}, owners, edges, map[string]bool{})
		if rule == "" {
			continue
		}
		violations = append(violations, Violation{
			Rule: rule, File: source.File, Line: importLine(source, dependency), Symbol: dependency,
			DependencyChain: chain, Message: reason,
		})
	}
	return violations
}

func forbiddenImportChain(
	source PackageOwnership, chain []string, owners map[string]PackageOwnership,
	edges map[string][]string, visited map[string]bool,
) ([]string, string, string) {
	target := chain[len(chain)-1]
	if visited[target] {
		return nil, "", ""
	}
	visited[target] = true
	if owner, found := owners[target]; found {
		if rule, reason := forbiddenLayerEdge(source, owner); rule != "" {
			return chain, rule, reason
		}
	}
	for _, dependency := range edges[target] {
		next := append(append([]string(nil), chain...), dependency)
		if found, rule, reason := forbiddenImportChain(source, next, owners, edges, visited); rule != "" {
			return found, rule, reason
		}
	}
	return nil, "", ""
}

func forbiddenLayerEdge(source, target PackageOwnership) (string, string) {
	if slices.Contains([]string{"testkit", "tool", "fixture"}, target.Layer) {
		return "AR01", "production reaches a test or tool source"
	}
	allowed := map[string][]string{
		"model":      {"model", "capability", "foundation"},
		"capability": {"model", "capability", "foundation"},
		"foundation": {"foundation"},
		"service":    {"service", "model", "capability", "foundation"},
		"adapter":    {"adapter", "model", "capability", "foundation"},
		"repo":       {"repo", "model", "capability", "foundation"},
	}
	if layers, restricted := allowed[source.Layer]; restricted && !slices.Contains(layers, target.Layer) {
		return "AR02", "forbidden layer dependency from " + source.Layer + " to " + target.Layer
	}
	if source.Layer == "service" && target.Layer == "service" && source.Module != target.Module {
		return "AR08", "concrete dependency between service modules"
	}
	return "", ""
}

func equalViolation(left, right Violation) bool {
	return left.Rule == right.Rule && left.File == right.File && left.Line == right.Line &&
		left.Symbol == right.Symbol && left.Message == right.Message
}

func importLine(source GoSourceInventory, dependency string) int {
	for _, imported := range source.ImportLocations {
		if imported.Path == dependency {
			return imported.Line
		}
	}
	return 1
}
