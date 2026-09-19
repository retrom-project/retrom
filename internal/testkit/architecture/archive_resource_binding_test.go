package architecture

import (
	"strings"
	"testing"
)

func TestArchiveResourceRequiresLiveCompiledBinding(t *testing.T) {
	t.Parallel()
	root, owners := newArchiveFixture(t)
	ports, violations := inspectArchiveFixture(t, root, owners, "default")
	proof := requireArchiveProof(t, ports, violations)
	if proof.Build != "default" || !strings.Contains(proof.Bindings[0].Factory.Symbol, "Factory).Open") {
		t.Fatalf("wrong build or factory method: %+v", proof)
	}
}

func TestArchiveResourceResolvesAliasesAndOptions(t *testing.T) {
	t.Parallel()
	root, owners := newArchiveFixture(t,
		archiveFixtureEdit{"internal/model/libraryimport/ports.go", "type Archives", "type ReaderAlias = StreamCursor\n type Archives"},
		archiveFixtureEdit{"internal/model/libraryimport/ports.go", "(StreamCursor, error)", "(ReaderAlias, error)"},
		archiveFixtureEdit{
			"internal/service/libraryimport/service.go",
			"func New(archives model.Archives) *Service { return &Service{archives: archives} }",
			"type Options struct { Archives model.Archives }\nfunc New(options Options) *Service { value := &Service{archives: options.Archives}; return value }",
		},
		archiveFixtureEdit{
			"internal/bootstrap/composition/creation.go",
			"return service.New(archive.New())", "factory := archive.New(); return service.New(service.Options{Archives: factory})",
		},
	)
	ports, violations := inspectArchiveFixture(t, root, owners, "default")
	requireArchiveProof(t, ports, violations)
}

func TestArchiveResourceAllowsClosedAcquisitionInputs(t *testing.T) {
	t.Parallel()
	root, owners := newArchiveFixture(t,
		archiveFixtureEdit{"internal/model/libraryimport/ports.go", "Open(context.Context)", "Open(context.Context, string)"},
		archiveFixtureEdit{"internal/adapter/archive/archive.go", "Open(ctx context.Context)", "Open(ctx context.Context, path string)"},
		archiveFixtureEdit{"internal/service/libraryimport/service.go", "service.archives.Open(ctx)", "service.archives.Open(ctx, \"fixture.zip\")"},
	)
	ports, violations := inspectArchiveFixture(t, root, owners, "default")
	requireArchiveProof(t, ports, violations)
}

func TestArchiveResourceAllowsClosedBootstrapLiteral(t *testing.T) {
	t.Parallel()
	root, owners := newArchiveFixture(t,
		archiveFixtureEdit{"internal/bootstrap/composition/creation.go", "archive.New()", "&archive.Factory{}"},
	)
	ports, violations := inspectArchiveFixture(t, root, owners, "default")
	requireArchiveProof(t, ports, violations)
}

func TestArchiveResourceRejectsVariadicAcquisition(t *testing.T) {
	t.Parallel()
	root, owners := newArchiveFixture(t,
		archiveFixtureEdit{"internal/model/libraryimport/ports.go", "Open(context.Context)", "Open(context.Context, ...string)"},
		archiveFixtureEdit{"internal/adapter/archive/archive.go", "Open(ctx context.Context)", "Open(ctx context.Context, paths ...string)"},
	)
	ports, violations := inspectArchiveFixture(t, root, owners, "default")
	requireArchiveRejected(t, ports, violations, "fixed context and closed-value parameter list")
}

func TestArchiveResourceKeepsExistingStreamAndLeaseRules(t *testing.T) {
	t.Parallel()
	root, owners := newArchiveFixture(t,
		archiveFixtureEdit{"internal/model/libraryimport/controls.go", "", `package libraryimport
import ("context"; "io")
type Stream interface { OpenStream(context.Context) (io.ReadCloser, error) }
type Lease interface { Close() error }
type Lock interface { Acquire() (Lease, error) }
`},
	)
	ports, violations := inspectArchiveFixture(t, root, owners, "default")
	requireArchiveProof(t, ports, violations)
	for _, port := range ports {
		if strings.HasSuffix(port.Symbol, ".OpenStream") || strings.HasSuffix(port.Symbol, ".Acquire") {
			if len(port.ArchiveResources) != 0 {
				t.Fatalf("existing resource rules were replaced: %+v", port)
			}
		}
	}
}

func TestArchiveResourceProvesEachBuildIndependently(t *testing.T) {
	t.Parallel()
	root, owners := newArchiveFixture(t,
		archiveFixtureEdit{"internal/adapter/archive/archive.go", "", "//go:build !integration\n\n" + archiveAdapterFixture},
		archiveFixtureEdit{
			"internal/adapter/archive/archive_integration.go", "",
			"//go:build integration\n\n" + strings.ReplaceAll(archiveAdapterFixture, "cursor", "taggedCursor"),
		},
	)
	var both []PortInventory
	for _, build := range []string{"default", "integration"} {
		ports, violations := inspectArchiveFixture(t, root, owners, build)
		requireArchiveProof(t, ports, violations)
		both = append(both, ports...)
	}
	merged := mergePortBuilds(both)
	port := findImplementationPort(t, merged, "(retrom/internal/model/libraryimport.Archives).Open")
	if len(port.ArchiveResources) != 2 || port.ArchiveResources[0].Build == port.ArchiveResources[1].Build {
		t.Fatalf("one build's proof was lost: %+v", port.ArchiveResources)
	}
}
