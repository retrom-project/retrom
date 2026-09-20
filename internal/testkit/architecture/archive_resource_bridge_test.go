package architecture

import (
	"strings"
	"testing"
)

const archiveOrdinaryBridgeFixture = `package httpapi
import ("retrom/internal/bootstrap/composition"; service "retrom/internal/service/libraryimport")
func New(label string, unrelated ...func()) *service.Service { return composition.Build() }
`

func archiveBridgeEdits(arguments string) []archiveFixtureEdit {
	return []archiveFixtureEdit{
		{"internal/transport/httpapi/bridge.go", "", archiveOrdinaryBridgeFixture},
		{"cmd/check/main.go", "", `package main
import ("context"; "retrom/internal/transport/httpapi")
func main() { service := httpapi.New(` + arguments + `); _ = service.Run(context.Background()) }
`},
	}
}

func archiveBridgeOwners(owners OwnershipRegistry) OwnershipRegistry {
	owners.Packages = append(owners.Packages, PackageOwnership{
		Path: "internal/transport/httpapi", Layer: "transport", Module: "httpapi", Owner: "RF04",
	})
	return owners
}

func TestArchiveResourceAllowsUnrelatedVariadicBridge(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, arguments string }{
		{"no-optional-arguments", `"api"`},
		{"unrelated-callback", `"api", func(){}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root, owners := newArchiveFixture(t, archiveBridgeEdits(test.arguments)...)
			ports, violations := inspectArchiveFixture(t, root, archiveBridgeOwners(owners), "default")
			requireArchiveProof(t, ports, violations)
		})
	}
}

func TestArchiveResourceRejectsUnknownVariadicBridgeOrigins(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		edits []archiveFixtureEdit
	}{
		{"factory-from-varargs", archiveFactoryVariadicBridgeFixture()},
		{"reader-from-varargs", archiveReaderVariadicBridgeFixture()},
		{"unknown-splat", []archiveFixtureEdit{
			{"cmd/check/main.go", "", `package main
import ("context"; "retrom/internal/transport/httpapi")
var unknown []func()
func main() { service := httpapi.New("api", unknown...); _ = service.Run(context.Background()) }
`},
		}},
		{"hidden-business-method", []archiveFixtureEdit{
			{"internal/adapter/archive/archive.go", "", archiveAdapterFixture + "\nfunc (*cursor) CommitBusiness() error { return nil }\n"},
		}},
		{"variadic-factory-constructor", []archiveFixtureEdit{
			{"internal/adapter/archive/archive.go", "func New()", "func New(unrelated ...string)"},
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			edits := append(archiveBridgeEdits(`"api", func(){}`), test.edits...)
			root, owners := newArchiveFixture(t, edits...)
			ports, violations := inspectArchiveFixture(t, root, archiveBridgeOwners(owners), "default")
			requireArchiveRejected(t, ports, violations, "")
		})
	}
}

func archiveFactoryVariadicBridgeFixture() []archiveFixtureEdit {
	return []archiveFixtureEdit{
		{"internal/bootstrap/composition/creation.go", "", `package composition
import ("retrom/internal/adapter/archive"; model "retrom/internal/model/libraryimport"; service "retrom/internal/service/libraryimport")
func Build(factory model.Archives) *service.Service { return service.New(factory) }
func KnownFactory() model.Archives { return archive.New() }
`},
		{"internal/transport/httpapi/bridge.go", "", `package httpapi
import ("retrom/internal/bootstrap/composition"; model "retrom/internal/model/libraryimport"; service "retrom/internal/service/libraryimport")
func New(archives ...model.Archives) *service.Service { return composition.Build(archives[0]) }
`},
		{"cmd/check/main.go", "", `package main
import ("context"; "retrom/internal/bootstrap/composition"; "retrom/internal/transport/httpapi")
func main() { service := httpapi.New(composition.KnownFactory()); _ = service.Run(context.Background()) }
`},
	}
}

func archiveReaderVariadicBridgeFixture() []archiveFixtureEdit {
	edits := injectedArchiveFixture(false)
	edits[len(edits)-1] = archiveFixtureEdit{
		"internal/bootstrap/composition/creation.go", "", `package composition
import ("io"; "retrom/internal/adapter/archive"; service "retrom/internal/service/libraryimport")
func Build(input io.Reader) *service.Service { return service.New(archive.New(input)) }
`,
	}
	return append(edits,
		archiveFixtureEdit{"internal/transport/httpapi/bridge.go", "", `package httpapi
import ("io"; "retrom/internal/bootstrap/composition"; service "retrom/internal/service/libraryimport")
func New(readers ...io.Reader) *service.Service { return composition.Build(readers[0]) }
`},
		archiveFixtureEdit{"cmd/check/main.go", "", `package main
import ("context"; "retrom/internal/transport/httpapi"; service "retrom/internal/service/libraryimport")
func main() { value := httpapi.New(&service.Business{}); _ = value.Run(context.Background()) }
`},
	)
}

func TestArchiveResourceRejectsPropagatedVariadicSlot(t *testing.T) {
	t.Parallel()
	edits := archiveFactoryVariadicBridgeFixture()
	for index := range edits {
		if edits[index].file == "internal/transport/httpapi/bridge.go" {
			edits[index].after = strings.ReplaceAll(edits[index].after,
				"return composition.Build(archives[0])", "return forward(archives)") +
				"\nfunc forward(values []model.Archives) *service.Service { return composition.Build(values[0]) }\n"
		}
	}
	root, owners := newArchiveFixture(t, edits...)
	ports, violations := inspectArchiveFixture(t, root, archiveBridgeOwners(owners), "default")
	requireArchiveRejected(t, ports, violations, "")
}

func TestArchiveResourceKeepsFixedBridgeParameterOrigin(t *testing.T) {
	t.Parallel()
	edits := archiveFactoryVariadicBridgeFixture()
	for index := range edits {
		switch edits[index].file {
		case "internal/transport/httpapi/bridge.go":
			edits[index].after = strings.ReplaceAll(edits[index].after,
				"New(archives ...model.Archives)", "New(factory model.Archives, unrelated ...func())")
			edits[index].after = strings.ReplaceAll(edits[index].after, "archives[0]", "factory")
		case "cmd/check/main.go":
			edits[index].after = strings.ReplaceAll(edits[index].after,
				"httpapi.New(composition.KnownFactory())", "httpapi.New(composition.KnownFactory(), func(){})")
		}
	}
	root, owners := newArchiveFixture(t, edits...)
	ports, violations := inspectArchiveFixture(t, root, archiveBridgeOwners(owners), "default")
	requireArchiveProof(t, ports, violations)
}

func TestArchiveResourceLeavesDemandedVariadicSlotUnbound(t *testing.T) {
	t.Parallel()
	root, owners := newArchiveFixture(t,
		archiveFixtureEdit{"internal/adapter/archive/archive.go", "type Factory struct{}", "type Factory []string"},
		archiveFixtureEdit{"internal/bootstrap/composition/creation.go", "", `package composition
import ("retrom/internal/adapter/archive"; service "retrom/internal/service/libraryimport")
func Build(parts ...string) *service.Service {
 factory := archive.Factory(parts)
 return service.New(&factory)
}
`},
		archiveFixtureEdit{"cmd/check/main.go", "composition.Build()", `composition.Build("local-file")`},
	)
	ports, violations := inspectArchiveFixture(t, root, owners, "default")
	requireArchiveRejected(t, ports, violations, "origin is unbound")
}
