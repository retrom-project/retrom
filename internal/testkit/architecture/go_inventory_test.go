package architecture

import (
	"errors"
	"strings"
	"testing"
)

func TestTypedInventoryResolvesAliasesAndTaggedTests(t *testing.T) {
	t.Parallel()
	root := newInventoryRepository(t)
	writeInventoryFile(t, root, "go.mod", "module retrom/fixture\n\ngo 1.26.5\n")
	writeInventoryFile(t, root, "value.go", `package fixture
type Box[T any] struct { Value T }
type Alias = Box[func()]
type Port interface { Save(Alias) error }
func Use(port Port) error { return port.Save(Alias{}) }
`)
	writeInventoryFile(t, root, "tagged_test.go", `//go:build integration

package fixture
import "testing"
func TestTagged(t *testing.T) {}
`)
	graph, err := InspectGo(t.Context(), root, []string{"value.go", "tagged_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	alias, portReference, tagged := false, false, false
	for _, pkg := range graph {
		for _, symbol := range pkg.Symbols {
			alias = alias || (symbol.Kind == "alias" && strings.Contains(symbol.Name, ".Alias"))
			tagged = tagged || (pkg.Build == "integration" && strings.HasSuffix(symbol.Name, ".TestTagged"))
			if pkg.Build == "default" && strings.HasSuffix(symbol.Name, ".TestTagged") {
				t.Fatal("integration test leaked into the default build")
			}
		}
		for _, reference := range pkg.References {
			portReference = portReference || strings.Contains(reference.Target, ".Port).Save")
		}
	}
	if !alias || !portReference || !tagged {
		t.Fatalf("incomplete graph: alias=%t portReference=%t tagged=%t", alias, portReference, tagged)
	}
}

func TestTypedInventoryIncludesExternalTestOnlyPackages(t *testing.T) {
	t.Parallel()
	root := newInventoryRepository(t)
	writeInventoryFile(t, root, "go.mod", "module retrom/fixture\n\ngo 1.26.5\n")
	writeInventoryFile(t, root, "boundary_test.go", "package fixture_test\nimport \"testing\"\nfunc TestBoundary(t *testing.T) {}\n")
	graph, err := InspectGo(t.Context(), root, []string{"boundary_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range graph {
		for _, symbol := range pkg.Symbols {
			if strings.HasSuffix(symbol.Name, ".TestBoundary") {
				return
			}
		}
	}
	t.Fatal("external test-only package was omitted")
}

func TestTypedInventoryIncludesTransitiveLocalSources(t *testing.T) {
	t.Parallel()
	root := newInventoryRepository(t)
	writeInventoryFile(t, root, "go.mod", "module retrom/fixture\n\ngo 1.26.5\n")
	writeInventoryFile(t, root, "entry.go", "package fixture\nimport \"retrom/fixture/helper\"\nvar Value = helper.Value\n")
	writeInventoryFile(t, root, "helper/value.go", "package helper\nconst Value = 1\n")
	graph, err := InspectGo(t.Context(), root, []string{"entry.go"})
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range graph {
		if pkg.ImportPath == "retrom/fixture/helper" {
			violations := ValidateCompilationScope([]string{"entry.go"}, graph)
			if len(violations) != 1 || violations[0].File != "helper/value.go" || violations[0].Rule != "AR01" {
				t.Fatalf("hidden compiled source escaped the coverage rule: %+v", violations)
			}
			return
		}
	}
	t.Fatal("local production dependency absent from the explicit source list was omitted")
}

func TestTypedInventoryRejectsIncompleteAnalysis(t *testing.T) {
	t.Parallel()
	root := newInventoryRepository(t)
	writeInventoryFile(t, root, "go.mod", "module retrom/fixture\n\ngo 1.26.5\n")
	writeInventoryFile(t, root, "invalid.go", "package fixture\nvar Broken = MissingType{}\n")
	if _, err := InspectGo(t.Context(), root, []string{"invalid.go"}); !errors.Is(err, ErrTypeAnalysis) {
		t.Fatalf("type error was not an analysis failure: %v", err)
	}
	if _, err := InspectGo(t.Context(), root, []string{"web/example.ts"}); !errors.Is(err, ErrEmptySources) {
		t.Fatalf("empty Go graph accepted: %v", err)
	}
}

func TestOwnershipJSONRejectsUnknownFieldsAndTrailingDocuments(t *testing.T) {
	t.Parallel()
	for _, content := range []string{
		`{"schemaVersion":1,"baseline":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","packages":[],"ignoreAll":true}`,
		`{"schemaVersion":1,"baseline":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","packages":[]} {}`,
		`{"schemaVersion":1,"schemaVersion":2,"baseline":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","packages":[]}`,
		`{"schemaVersion":1,"baseline":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","packages":[{"path":"a","path":"b"}]}`,
	} {
		t.Run(content, func(t *testing.T) {
			root := t.TempDir()
			writeInventoryFile(t, root, "quality/architecture/package-ownership.json", content)
			if _, err := LoadOwnership(root); err == nil {
				t.Fatal("invalid ownership input accepted")
			}
		})
	}
}

func TestOwnershipCannotRelabelProductionAsTestkit(t *testing.T) {
	t.Parallel()
	entry := PackageOwnership{
		Path: "internal/service/hidden", Layer: "testkit", Module: "hidden", Owner: "RF21",
		Files: []OwnedFile{{Path: "internal/service/hidden/hidden.go", Kind: "production"}},
	}
	registry := OwnershipRegistry{SchemaVersion: 1, Baseline: strings.Repeat("a", 40), Packages: []PackageOwnership{entry}}
	issues := ValidateOwnership([]string{entry.Files[0].Path}, registry)
	if len(issues) != 1 || issues[0].Rule != "GOV01" {
		t.Fatalf("source relabeling accepted: %+v", issues)
	}
}

func TestSourceAnalysisRejectsConcurrentAddition(t *testing.T) {
	t.Parallel()
	root := newInventoryRepository(t)
	writeInventoryFile(t, root, "before.go", "package fixture\n")
	before, err := SnapshotFiles(root, []string{"before.go"})
	if err != nil {
		t.Fatal(err)
	}
	writeInventoryFile(t, root, "after.go", "package fixture\n")
	if err := verifyUnchangedSourceSet(t.Context(), root, before); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("new source added during analysis escaped: %v", err)
	}
}

func TestBoundaryReportNeverPassesIncompleteChecks(t *testing.T) {
	t.Parallel()
	if code := BoundaryExitCode(BoundaryReport{}); code != 2 {
		t.Fatalf("empty analysis exit=%d", code)
	}
	report := BoundaryReport{Status: "NOT_READY", SourceSHA256: strings.Repeat("a", 64), Checks: []string{"partial"}}
	if code := BoundaryExitCode(report); code != 1 {
		t.Fatalf("partial analysis exit=%d", code)
	}
	report.Status = "VERIFIED"
	report.Pending = []string{"unimplemented"}
	if code := BoundaryExitCode(report); code != 1 {
		t.Fatalf("incomplete verified report exit=%d", code)
	}
}
