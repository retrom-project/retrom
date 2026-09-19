package architecture

import (
	"testing"
)

func TestDependencyRulesTraceHiddenImplementationAndTestkit(t *testing.T) {
	t.Parallel()
	owners := OwnershipRegistry{Packages: []PackageOwnership{
		{Path: "internal/model/command", Layer: "model", Module: "command"},
		{Path: "internal/foundation/helper", Layer: "foundation", Module: "helper"},
		{Path: "internal/adapter/files", Layer: "adapter", Module: "files"},
		{Path: "internal/service/saves", Layer: "service", Module: "saves"},
		{Path: "internal/service/launch", Layer: "service", Module: "launch"},
		{Path: "internal/testkit/hidden", Layer: "testkit", Module: "hidden"},
	}}
	sources := []GoSourceInventory{
		{File: "internal/model/command/value.go", Imports: []string{"retrom/internal/foundation/helper"}},
		{File: "internal/foundation/helper/hidden.go", Imports: []string{"retrom/internal/adapter/files"}},
		{File: "internal/service/saves/save.go", Imports: []string{"retrom/internal/service/launch"}},
		{File: "internal/adapter/files/open.go", Imports: []string{"retrom/internal/testkit/hidden"}},
		{File: "internal/model/command/value_test.go", Imports: []string{"retrom/internal/adapter/files"}},
	}
	violations := InspectDependencyRules(sources, owners)
	found := make(map[string]Violation)
	for _, violation := range violations {
		found[violation.File] = violation
	}
	if got := found[sources[0].File]; got.Rule != "AR02" || len(got.DependencyChain) != 3 {
		t.Fatalf("indirect implementation dependency escaped: %+v", got)
	}
	if got := found[sources[2].File]; got.Rule != "AR08" {
		t.Fatalf("cross-service dependency escaped: %+v", got)
	}
	if got := found[sources[3].File]; got.Rule != "AR01" {
		t.Fatalf("production testkit dependency escaped: %+v", got)
	}
	if _, exists := found[sources[4].File]; exists {
		t.Fatal("test-only dependency incorrectly treated as production")
	}
}

func TestDependencyRulesAcceptPureCyclesWithoutHanging(t *testing.T) {
	t.Parallel()
	owners := OwnershipRegistry{Packages: []PackageOwnership{
		{Path: "internal/model/value", Layer: "model"},
		{Path: "internal/capability/parse", Layer: "capability"},
	}}
	sources := []GoSourceInventory{
		{File: "internal/model/value/value.go", Imports: []string{"retrom/internal/capability/parse"}},
		{File: "internal/capability/parse/parse.go", Imports: []string{"retrom/internal/model/value"}},
	}
	// Import cycles are rejected by the mandatory type loader. This graph pass
	// must remain bounded when examining inactive build configurations.
	if violations := InspectDependencyRules(sources, owners); len(violations) != 0 {
		t.Fatalf("pure layer edges were rejected: %+v", violations)
	}
}
