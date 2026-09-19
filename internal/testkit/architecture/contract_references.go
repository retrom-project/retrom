package architecture

import (
	"slices"
	"strings"
)

func inspectOperationReferences(record OperationRecord, index contractIndex) []Violation {
	symbols := append([]string{record.Snapshot, record.Prepare, record.Commit}, record.Consumers...)
	symbols = append(symbols, record.Policies...)
	tests := slices.Clone(record.Tests)
	for _, fact := range record.Facts {
		symbols = append(symbols, fact.Reader)
	}
	for _, guard := range record.Guards {
		tests = append(tests, guard.Test)
	}
	for _, write := range record.Writes {
		symbols = append(symbols, write.Helper)
		tests = append(tests, write.Test)
	}
	for _, effect := range record.AfterCommit {
		tests = append(tests, effect.Test)
	}
	content := record.ContentCoordination
	for _, symbol := range []*string{content.Acquire, content.Release, content.ProtectedCommit} {
		if symbol != nil {
			symbols = append(symbols, *symbol)
		}
	}
	tests = append(tests, content.Tests...)
	violations := inspectRegisteredSymbols("operations.json", record.ID, symbols, index)
	return append(violations, inspectRegisteredTests("operations.json", record.ID, tests, index)...)
}

func inspectRegisteredSymbols(file, id string, symbols []string, index contractIndex) []Violation {
	violations := make([]Violation, 0)
	for _, name := range symbols {
		found := false
		for _, symbol := range index.symbols[name] {
			if !strings.HasSuffix(symbol.File, "_test.go") && !strings.Contains(symbol.File, "/testkit/") {
				found = true
			}
		}
		if !found {
			violations = append(violations, contractViolation(file, id, "production definition is absent: "+name))
		}
	}
	return violations
}

func inspectRegisteredTests(file, id string, tests []string, index contractIndex) []Violation {
	violations := make([]Violation, 0)
	for _, name := range tests {
		if _, exists := index.tests[name]; !exists {
			violations = append(violations, contractViolation(file, id, "unregistered test reference: "+name))
		}
	}
	return violations
}

func inspectPolicyReferences(registry PolicyRegistry, index contractIndex) []Violation {
	violations := make([]Violation, 0)
	if registry.SchemaVersion != 1 || !registry.InventoryComplete || len(registry.Policies) == 0 {
		violations = append(violations, contractViolation("policies.json", "", "policy census is incomplete"))
	}
	seenIDs := make(map[string]bool)
	seenDefinitions := make(map[string]bool)
	for _, policy := range registry.Policies {
		if policy.ID == "" || policy.Owner == "" || policy.Invariant == "" || len(policy.Consumers) == 0 ||
			seenIDs[policy.ID] || seenDefinitions[policy.Symbol] {
			violations = append(violations, contractViolation("policies.json", policy.ID, "invalid or duplicate policy"))
		}
		seenIDs[policy.ID], seenDefinitions[policy.Symbol] = true, true
		symbols := append([]string{policy.Symbol}, policy.Consumers...)
		violations = append(violations, inspectRegisteredSymbols("policies.json", policy.ID, symbols, index)...)
		violations = append(violations, inspectRegisteredTests("policies.json", policy.ID, policy.Tests, index)...)
	}
	return violations
}
