package architecture

import (
	"context"
	"slices"
	"strings"
)

// BoundaryReport is deliberately NOT_READY until every permanent rule is implemented
// and the complete production graph is clean. Partial checks never imply verification.
type BoundaryReport struct {
	SchemaVersion       int                 `json:"schemaVersion"`
	Status              string              `json:"status"`
	Baseline            BaselineReport      `json:"baseline"`
	SourceSHA256        string              `json:"sourceSha256"`
	CompiledSHA256      string              `json:"compiledSha256"`
	ConfigurationSHA256 string              `json:"configurationSha256"`
	Checks              []string            `json:"implementedChecks"`
	Pending             []string            `json:"pendingChecks"`
	Ports               []PortInventory     `json:"ports"`
	Functions           []FunctionInventory `json:"functions"`
	Violations          []Violation         `json:"violations"`
}

// InspectBoundaries runs the currently implemented semantic checks over all builds.
func InspectBoundaries(ctx context.Context, root string, inventory InventoryReport) (BoundaryReport, error) {
	registry, err := LoadOwnership(root)
	if err != nil {
		return BoundaryReport{}, err
	}
	sources := make([]string, 0, len(inventory.Sources.Files))
	for _, source := range inventory.Sources.Files {
		sources = append(sources, source.Path)
	}
	ports := make([]PortInventory, 0)
	functions := make([]FunctionInventory, 0)
	violations := append([]Violation(nil), inventory.Violations...)
	violations = append(violations, InspectDependencyRules(inventory.GoSources, registry)...)
	for _, build := range []string{"default", "integration"} {
		graph, err := loadInventoryGraph(ctx, root, goInventoryPatterns(sources), build)
		if err != nil {
			return BoundaryReport{}, err
		}
		nextPorts, nextViolations := inspectPortGraph(root, graph, registry)
		ports = append(ports, nextPorts...)
		nextFunctions := inspectFunctionGraph(root, graph, registry)
		functions = append(functions, nextFunctions...)
		violations = append(violations, inspectExecutionRules(nextFunctions)...)
		violations = append(violations, nextViolations...)
	}
	if err := verifyUnchangedSourceSet(ctx, root, inventory.Sources); err != nil {
		return BoundaryReport{}, err
	}
	if err := verifyUnchangedSources(root, inventory.Configuration); err != nil {
		return BoundaryReport{}, err
	}
	slices.SortFunc(ports, func(left, right PortInventory) int { return strings.Compare(left.Symbol, right.Symbol) })
	ports = slices.CompactFunc(ports, func(left, right PortInventory) bool {
		return left.Symbol == right.Symbol && left.Signature == right.Signature
	})
	slices.SortFunc(functions, func(left, right FunctionInventory) int {
		return strings.Compare(left.Symbol, right.Symbol)
	})
	functions = slices.CompactFunc(functions, func(left, right FunctionInventory) bool {
		return left.Symbol == right.Symbol && left.File == right.File
	})
	attachPortConsumers(ports, functions)
	slices.SortFunc(violations, compareViolations)
	violations = slices.CompactFunc(violations, equalViolation)
	return BoundaryReport{
		SchemaVersion: 1, Status: "NOT_READY", Baseline: inventory.Baseline,
		SourceSHA256: inventory.Sources.SHA256, CompiledSHA256: inventory.CompiledInputs.SHA256,
		ConfigurationSHA256: inventory.Configuration.SHA256,
		Checks: []string{
			"source ownership", "compatibility inputs", "layer dependencies", "model port value graphs",
			"resolved execution graph", "known I/O and SQL execution effects",
		},
		Pending: pendingBoundaryChecks(), Ports: ports, Functions: functions, Violations: violations,
	}, nil
}

func pendingBoundaryChecks() []string {
	return []string{
		"generated provenance and supported build registration",
		"direct repository boundaries and constructor injection",
		"transitive executable purity and SQL capability calls",
		"construction and lifecycle ownership",
		"reexports, atomic helper consumers, and policy uniqueness",
		"technical clock sampling and protocol privacy",
		"operation, job, dispatcher, test, and evidence registries",
		"frontend feature and server boundaries",
		"test integrity and permanent gate wiring",
	}
}

// BoundaryExitCode never reports a partially implemented check as successful.
func BoundaryExitCode(report BoundaryReport) int {
	if len(report.Checks) == 0 || report.SourceSHA256 == "" {
		return 2
	}
	if len(report.Pending) > 0 || len(report.Violations) > 0 || report.Status != "VERIFIED" {
		return 1
	}
	return 0
}
