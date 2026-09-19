package architecture

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"
)

// The machine cases have a 10 s hard limit. Leave time for failure reporting;
// loading types invokes go list, never a nested go test or a persisted report.
const rf03AnalysisBudget = 8 * time.Second

type rf03Repository struct {
	root          string
	sources       SourceSnapshot
	configuration SourceSnapshot
	syntax        []GoSourceInventory
	ownership     OwnershipRegistry
	graphs        [][]*packages.Package
}

var rf03RepositoryCache struct {
	sync.Mutex

	value *rf03Repository
}

func rf03CurrentRepository(t *testing.T) *rf03Repository {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), rf03AnalysisBudget)
	defer cancel()
	root := filepath.Clean(filepath.Join(PackageDirectory(t), "../../.."))
	sources, err := DiscoverSources(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	before, err := SnapshotFiles(root, sources)
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := SnapshotFiles(root, []string{
		"go.mod", "go.sum", "quality/architecture/package-ownership.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	rf03RepositoryCache.Lock()
	defer rf03RepositoryCache.Unlock()
	if cached := rf03RepositoryCache.value; cached != nil && cached.root == root &&
		cached.sources.SHA256 == before.SHA256 && cached.configuration.SHA256 == configuration.SHA256 {
		return cached
	}
	ownership, err := LoadOwnership(root)
	if err != nil {
		t.Fatal(err)
	}
	rf03AssertNoFindings(t, ValidateOwnership(sources, ownership))
	syntax, err := InspectGoSyntax(root, sources)
	if err != nil {
		t.Fatal(err)
	}
	result := &rf03Repository{
		root: root, sources: before, configuration: configuration, syntax: syntax, ownership: ownership,
	}
	compiled := make([]GoPackageInventory, 0)
	patterns := goInventoryPatterns(sources)
	for _, build := range []string{"default", "integration"} {
		graph, loadErr := loadInventoryGraph(ctx, root, patterns, build)
		if loadErr != nil {
			t.Fatalf("%s current repository type analysis: %v", build, loadErr)
		}
		result.graphs = append(result.graphs, graph)
		compiled = append(compiled, rf03CompiledPackages(t, root, build, graph)...)
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("RF03 analysis exceeded %s: %v", rf03AnalysisBudget, err)
	}
	rf03AssertNoFindings(t, ValidateCompilationScope(sources, compiled))
	// Each caller verifies the same source/configuration snapshot after its own
	// assertions. Do not hash the whole tree twice at the end of one case.
	rf03RepositoryCache.value = result
	return result
}

// Compilation coverage only consumes files. Keep the complete typed graphs for
// both cases without rebuilding unused, sorted symbol/reference inventories.
func rf03CompiledPackages(
	t *testing.T, root, build string, graph []*packages.Package,
) []GoPackageInventory {
	t.Helper()
	compiled := make([]GoPackageInventory, 0, len(graph))
	for _, pkg := range graph {
		item := GoPackageInventory{ID: pkg.ID, ImportPath: pkg.PkgPath, Build: build}
		for _, name := range pkg.CompiledGoFiles {
			relative, err := filepath.Rel(root, name)
			if err != nil || !safeRepositoryPath(filepath.ToSlash(relative)) {
				t.Fatalf("compiled source outside repository: %s (%v)", name, err)
			}
			item.Files = append(item.Files, filepath.ToSlash(relative))
		}
		compiled = append(compiled, item)
	}
	return compiled
}

func (repository *rf03Repository) assertUnchanged(t *testing.T) {
	t.Helper()
	if err := verifyUnchangedSourceSet(t.Context(), repository.root, repository.sources); err != nil {
		t.Fatal(err)
	}
	if err := verifyUnchangedSources(repository.root, repository.configuration); err != nil {
		t.Fatal(err)
	}
}

func rf03AssertNoFindings(t *testing.T, findings []Violation) {
	t.Helper()
	slices.SortFunc(findings, compareViolations)
	findings = slices.CompactFunc(findings, equalViolation)
	if len(findings) == 0 {
		return
	}
	for index, finding := range findings {
		if index == 50 {
			break
		}
		t.Logf("%s:%d %s %s: %s; origins=%s", finding.File, finding.Line, finding.Rule,
			finding.Symbol, finding.Message, strings.Join(finding.DependencyChain, " -> "))
	}
	t.Fatalf("current repository has %d RF03 findings; printed first %d", len(findings), min(50, len(findings)))
}
