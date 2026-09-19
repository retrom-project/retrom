package architecture

import (
	"context"
	"slices"
	"strings"
)

type ContractReport struct {
	SchemaVersion       int            `json:"schemaVersion"`
	Status              string         `json:"status"`
	Baseline            BaselineReport `json:"baseline"`
	SourceSHA256        string         `json:"sourceSha256"`
	ConfigurationSHA256 string         `json:"configurationSha256"`
	OperationCount      int            `json:"operationCount"`
	PolicyCount         int            `json:"policyCount"`
	TestCount           int            `json:"testCount"`
	Pending             []string       `json:"pendingChecks"`
	Violations          []Violation    `json:"violations"`
}

// InspectContracts verifies declarations against actual typed source. It never
// consumes final run evidence, so a static check cannot recursively need itself.
func InspectContracts(ctx context.Context, root string, inventory InventoryReport) (ContractReport, error) {
	var operations OperationRegistry
	if err := loadContractFile(root, "operations.json", &operations); err != nil {
		return ContractReport{}, err
	}
	var policies PolicyRegistry
	if err := loadContractFile(root, "policies.json", &policies); err != nil {
		return ContractReport{}, err
	}
	var cases VerificationRegistry
	if err := loadContractFile(root, "test-cases.json", &cases); err != nil {
		return ContractReport{}, err
	}
	schema, err := compileOperationSchema(root)
	if err != nil {
		return ContractReport{}, err
	}
	index := newContractIndex(inventory, cases)
	violations := inspectTestDefinitions(cases, index)
	if !operationCensusComplete(operations, inventory.Baseline.Baseline) {
		violations = append(violations, contractViolation("operations.json", "", "operation census is incomplete"))
	}
	seen := make(map[string]bool)
	for _, data := range operations.Operations {
		var record OperationRecord
		if err := decodeStrictJSON(data, &record); err != nil {
			return ContractReport{}, err
		}
		if err := validateOperationSchema(schema, data); err != nil {
			violations = append(violations, contractViolation("operations.json", record.ID, err.Error()))
		}
		if record.ID == "" || seen[record.ID] {
			violations = append(violations, contractViolation("operations.json", record.ID, "duplicate or empty operation ID"))
		}
		seen[record.ID] = true
		violations = append(violations, inspectOperationReferences(record, index)...)
	}
	violations = append(violations, inspectPolicyReferences(policies, index)...)
	if err := verifyUnchangedSourceSet(ctx, root, inventory.Sources); err != nil {
		return ContractReport{}, err
	}
	if err := verifyUnchangedSources(root, inventory.Configuration); err != nil {
		return ContractReport{}, err
	}
	slices.SortFunc(violations, compareViolations)
	violations = slices.CompactFunc(violations, equalViolation)
	return ContractReport{
		SchemaVersion: 1, Status: "NOT_READY", Baseline: inventory.Baseline,
		SourceSHA256: inventory.Sources.SHA256, ConfigurationSHA256: inventory.Configuration.SHA256,
		OperationCount: len(operations.Operations), PolicyCount: len(policies.Policies), TestCount: len(cases.Cases),
		Pending: []string{
			"whole-tree operation census and write-stage/guard correspondence",
			"policy consumer and definition completeness",
			"frontend leaf discovery, dispatcher and job registries",
			"test assertion integrity and runner DAG analysis",
		},
		Violations: violations,
	}, nil
}

func contractViolation(file, symbol, message string) Violation {
	return Violation{
		Rule: "GOV01", File: "quality/architecture/" + file, Line: 1, Symbol: symbol,
		DependencyChain: []string{symbol}, Message: message,
	}
}

type contractIndex struct {
	symbols map[string][]GoSymbol
	tests   map[string]VerificationCase
}

func newContractIndex(inventory InventoryReport, registry VerificationRegistry) contractIndex {
	index := contractIndex{symbols: make(map[string][]GoSymbol), tests: make(map[string]VerificationCase)}
	for _, pkg := range inventory.GoPackages {
		for _, symbol := range pkg.Symbols {
			index.symbols[symbol.Name] = append(index.symbols[symbol.Name], symbol)
		}
	}
	for _, test := range registry.Cases {
		index.tests[test.ID] = test
	}
	return index
}

func inspectTestDefinitions(registry VerificationRegistry, index contractIndex) []Violation {
	violations := make([]Violation, 0)
	if registry.SchemaVersion != 1 || !validCommitSHA(registry.Baseline) || len(registry.Cases) == 0 {
		violations = append(violations, contractViolation("test-cases.json", "", "invalid or empty test registry"))
	}
	seen := make(map[string]bool)
	for _, test := range registry.Cases {
		if seen[test.ID] || !test.Required || test.Deadline <= 0 || test.Deadline > 180 || !safeRepositoryPath(test.File) {
			violations = append(violations, contractViolation("test-cases.json", test.ID, "invalid test registration"))
		}
		seen[test.ID] = true
		if !strings.HasSuffix(test.File, ".go") {
			continue
		}

		if !hasNamedTestDefinition(test, index) {
			violations = append(violations, contractViolation("test-cases.json", test.ID, "named Go test definition is absent"))
		}
	}
	return violations
}

func operationCensusComplete(registry OperationRegistry, baseline string) bool {
	return registry.SchemaVersion == 1 && registry.Baseline == baseline &&
		registry.InventoryComplete && registry.Status == "VERIFIED" && len(registry.Pending) == 0
}

func hasNamedTestDefinition(test VerificationCase, index contractIndex) bool {
	for name, symbols := range index.symbols {
		for _, symbol := range symbols {
			if symbol.File == test.File && symbol.Kind == "function" &&
				strings.HasSuffix(name, "."+test.Symbol) && symbol.Type == "func(t *testing.T)" {
				return true
			}
		}
	}
	return false
}
