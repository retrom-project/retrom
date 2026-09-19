package architecture

import (
	"go/types"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestRefactorRF03_adapter_free(t *testing.T) {
	repository := rf03CurrentRepository(t)
	findings := make([]Violation, 0)
	for _, finding := range InspectDependencyRules(repository.syntax, repository.ownership) {
		if strings.HasPrefix(finding.File, "internal/model/") {
			findings = append(findings, finding)
		}
	}
	definitions, ports := 0, 0
	for _, graph := range repository.graphs {
		for _, pkg := range graph {
			if pkg.ID != pkg.PkgPath || !inPackageTree(pkg.PkgPath, "retrom/internal/model") {
				continue
			}
			for _, name := range pkg.Types.Scope().Names() {
				object := pkg.Types.Scope().Lookup(name)
				if !object.Exported() {
					continue
				}
				definitions++
				origin := inventoryPosition(repository.root, pkg.Fset, object.Pos())
				findings = append(findings, rf03DefinitionOrigins(object.Type(), origin.Filename, origin.Line,
					inventoryObjectID(object))...)
				if _, declared := object.Type().Underlying().(*types.Interface); declared {
					ports++
				}
			}
		}
	}
	if definitions == 0 || ports == 0 {
		t.Fatalf("empty Model definition/port analysis: definitions=%d ports=%d", definitions, ports)
	}
	// Follow the declared signatures above, never the objects that happen to
	// implement them. A Model port does not import its Adapter/Repo implementer.
	rf03AssertClockPortDeclaration(t, repository)
	repository.assertUnchanged(t)
	t.Logf("current Model definitions=%d interface declarations=%d, source=%s", definitions, ports,
		repository.sources.SHA256)
	rf03AssertNoFindings(t, findings)
}

func TestRefactorRF03_no_reexport(t *testing.T) {
	repository := rf03CurrentRepository(t)
	findings := make([]Violation, 0)
	servicePackages := make(map[string]bool)
	for _, graph := range repository.graphs {
		services := make([]*packages.Package, 0)
		for _, pkg := range graph {
			if pkg.ID == pkg.PkgPath && inPackageTree(pkg.PkgPath, "retrom/internal/service") {
				servicePackages[pkg.PkgPath] = true
				services = append(services, pkg)
				findings = append(findings, rf03ServiceAliases(repository.root, pkg)...)
			}
		}
		// Reuse the typed forwarding detector, including non-Model targets.
		// inspectReexports alone intentionally reports only Model forwarding.
		for _, declaration := range forwardingDeclarations(repository.root, services) {
			if !strings.HasPrefix(declaration.file, "internal/service/") || declaration.kind == "type alias" {
				continue
			}
			targets := make([]string, 0, len(declaration.targets))
			for _, target := range declaration.targets {
				targets = append(targets, inventoryObjectID(target))
			}
			slices.Sort(targets)
			targets = slices.Compact(targets)
			findings = append(findings, Violation{
				Rule: "AR08", File: declaration.file, Line: declaration.line,
				Symbol: inventoryObjectID(declaration.object), DependencyChain: targets,
				Message: "Service retains " + declaration.kind,
			})
		}
	}
	if len(servicePackages) == 0 {
		t.Fatal("no real Service packages were type-checked")
	}
	repository.assertUnchanged(t)
	t.Logf("current Service packages=%d, default/integration consumers type-checked, source=%s",
		len(servicePackages), repository.sources.SHA256)
	rf03AssertNoFindings(t, findings)
}

func rf03ServiceAliases(root string, pkg *packages.Package) []Violation {
	findings := make([]Violation, 0)
	for name, object := range pkg.TypesInfo.Defs {
		alias, ok := object.(*types.TypeName)
		if !ok || !alias.IsAlias() {
			continue
		}
		origin := inventoryPosition(root, pkg.Fset, name.Pos())
		if strings.HasSuffix(origin.Filename, "_test.go") {
			continue
		}
		findings = append(findings, Violation{
			Rule: "AR08", File: origin.Filename, Line: origin.Line, Symbol: inventoryObjectID(alias),
			DependencyChain: []string{types.TypeString(types.Unalias(alias.Type()), packageQualifier)},
			Message:         "Service retains type alias",
		})
	}
	return findings
}
