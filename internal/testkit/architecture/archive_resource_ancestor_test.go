package architecture

import (
	"strings"
	"testing"
)

const archiveAncestorWorkerFixture = `package worker
import ("context"; "retrom/internal/bootstrap/composition")
type Worker struct{}
func (worker *Worker) Start(ctx context.Context) { _ = composition.Build().Run(ctx) }
`

const archiveAncestorTransportFixture = `package httpapi
import ("context"; runner "retrom/internal/adapter/worker")
func New(ctx context.Context) { worker := &runner.Worker{}; worker.Start(ctx) }
`

func archiveAncestorEdits() []archiveFixtureEdit {
	return []archiveFixtureEdit{
		{"internal/adapter/worker/worker.go", "", archiveAncestorWorkerFixture},
		{"internal/transport/httpapi/bridge.go", "", archiveAncestorTransportFixture},
		{"cmd/check/main.go", "", `package main
import ("context"; "retrom/internal/transport/httpapi")
func main() { httpapi.New(context.Background()) }
`},
	}
}

func archiveAncestorOwners(owners OwnershipRegistry) OwnershipRegistry {
	owners = archiveBridgeOwners(owners)
	owners.Packages = append(owners.Packages,
		PackageOwnership{Path: "internal/adapter/worker", Layer: "adapter", Module: "worker", Owner: "RF04"},
		PackageOwnership{Path: "internal/bootstrap/entry", Layer: "bootstrap", Module: "entry", Owner: "RF04"},
	)
	return owners
}

func TestArchiveResourceAllowsOrdinaryAdapterAncestor(t *testing.T) {
	t.Parallel()
	for _, bootstrapEntry := range []bool{false, true} {
		name := "transport-entry"
		if bootstrapEntry {
			name = "bootstrap-entry"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			edits := archiveAncestorEdits()
			if bootstrapEntry {
				edits = append(edits,
					archiveFixtureEdit{"internal/bootstrap/entry/entry.go", "", strings.ReplaceAll(archiveAncestorTransportFixture, "package httpapi", "package entry")},
					archiveFixtureEdit{"cmd/check/main.go", "retrom/internal/transport/httpapi", "retrom/internal/bootstrap/entry"},
					archiveFixtureEdit{"cmd/check/main.go", "httpapi.New", "entry.New"},
				)
			}
			root, owners := newArchiveFixture(t, edits...)
			ports, violations := inspectArchiveFixture(t, root, archiveAncestorOwners(owners), "default")
			requireArchiveProof(t, ports, violations)
		})
	}
}

func TestArchiveResourceRejectsUntrustedAncestorOrigins(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		edits []archiveFixtureEdit
	}{
		{"factory-receiver", archiveAncestorFactoryReceiver("untrusted")},
		{"factory-field-write", archiveAncestorFactoryReceiver("rewrite")},
		{"factory-receiver-escape", archiveAncestorFactoryReceiver("escape")},
		{"factory-parameter", archiveAncestorFactoryParameter(false)},
		{"factory-parameter-escape", archiveAncestorFactoryParameter(true)},
		{"reader-receiver", archiveAncestorReaderFixture()},
		{"actual-factory-business-call", []archiveFixtureEdit{
			{"internal/adapter/archive/archive.go", `"context";`, `"context"; service "retrom/internal/service/libraryimport";`},
			{"internal/adapter/archive/archive.go", "return &Factory{}", "service.BusinessSideEffect(); return &Factory{}"},
			{"internal/service/libraryimport/business.go", "", "package libraryimport\nfunc BusinessSideEffect() {}\n"},
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			edits := append(archiveAncestorEdits(), test.edits...)
			root, owners := newArchiveFixture(t, edits...)
			ports, violations := inspectArchiveFixture(t, root, archiveAncestorOwners(owners), "default")
			requireArchiveRejected(t, ports, violations, "")
		})
	}
}

const archiveAncestorReplacementFixture = `package libraryimport
import ("context"; "errors"; model "retrom/internal/model/libraryimport")
type Replacement struct{}
func (Replacement) Open(context.Context) (model.StreamCursor,error) { return nil,errors.New("business factory") }
`

func archiveAncestorFactoryReceiver(mode string) []archiveFixtureEdit {
	bootstrap := `package composition
import ("retrom/internal/adapter/archive"; model "retrom/internal/model/libraryimport"; service "retrom/internal/service/libraryimport")
func Build(factory model.Archives) *service.Service { return service.New(factory) }
func KnownFactory() model.Archives { return archive.New() }
`
	worker := `package worker
import ("context"; "retrom/internal/bootstrap/composition"; model "retrom/internal/model/libraryimport")
type Worker struct{ Factory model.Archives }
func (worker *Worker) Start(ctx context.Context) { _ = composition.Build(worker.Factory).Run(ctx) }
`
	transport := `package httpapi
import ("context"; runner "retrom/internal/adapter/worker"; "retrom/internal/bootstrap/composition")
func New(ctx context.Context) { worker := &runner.Worker{Factory:composition.KnownFactory()}; worker.Start(ctx) }
`
	switch mode {
	case "untrusted":
		transport = strings.ReplaceAll(transport, `"retrom/internal/bootstrap/composition"`, `service "retrom/internal/service/libraryimport"`)
		transport = strings.ReplaceAll(transport, "composition.KnownFactory()", "service.Replacement{}")
	case "rewrite":
		worker = strings.ReplaceAll(worker, `"context";`, `"context"; service "retrom/internal/service/libraryimport";`)
		worker = strings.ReplaceAll(worker, "{ _ = composition.Build", "{ worker.Factory = service.Replacement{}; _ = composition.Build")
	case "escape":
		worker = strings.ReplaceAll(worker, "{ _ = composition.Build", "{ opaque(worker.Factory); _ = composition.Build")
		worker += "\nfunc opaque(factory model.Archives) {}\n"
	}
	return []archiveFixtureEdit{
		{"internal/bootstrap/composition/creation.go", "", bootstrap},
		{"internal/adapter/worker/worker.go", "", worker},
		{"internal/transport/httpapi/bridge.go", "", transport},
		{"internal/service/libraryimport/business.go", "", archiveAncestorReplacementFixture},
	}
}

func archiveAncestorFactoryParameter(escape bool) []archiveFixtureEdit {
	edits := archiveAncestorFactoryReceiver("untrusted")
	if escape {
		edits = archiveAncestorFactoryReceiver("known")
	}
	for index := range edits {
		switch edits[index].file {
		case "internal/adapter/worker/worker.go":
			source := edits[index].after
			source = strings.ReplaceAll(source, "type Worker struct{ Factory model.Archives }", "type Worker struct{}")
			source = strings.ReplaceAll(source, "Start(ctx context.Context)", "Start(ctx context.Context, factory model.Archives)")
			source = strings.ReplaceAll(source, "worker.Factory", "factory")
			if escape {
				source = strings.ReplaceAll(source, "{ _ = composition.Build", "{ opaque(factory); _ = composition.Build")
				source += "\nfunc opaque(factory model.Archives) {}\n"
			}
			edits[index].after = source
		case "internal/transport/httpapi/bridge.go":
			source := edits[index].after
			value := "service.Replacement{}"
			if escape {
				value = "composition.KnownFactory()"
			}
			source = strings.ReplaceAll(source, "&runner.Worker{Factory:"+value+"}", "&runner.Worker{}")
			source = strings.ReplaceAll(source, "worker.Start(ctx)", "worker.Start(ctx,"+value+")")
			edits[index].after = source
		}
	}
	return edits
}

func archiveAncestorReaderFixture() []archiveFixtureEdit {
	edits := injectedArchiveFixture(false)
	edits[len(edits)-1] = archiveFixtureEdit{"internal/bootstrap/composition/creation.go", "", `package composition
import ("io"; "retrom/internal/adapter/archive"; service "retrom/internal/service/libraryimport")
func Build(input io.Reader) *service.Service { return service.New(archive.New(input)) }
`}
	return append(edits,
		archiveFixtureEdit{"internal/adapter/worker/worker.go", "", `package worker
import ("context"; "io"; "retrom/internal/bootstrap/composition")
type Worker struct{ Input io.Reader }
func (worker *Worker) Start(ctx context.Context) { _ = composition.Build(worker.Input).Run(ctx) }
`},
		archiveFixtureEdit{"internal/transport/httpapi/bridge.go", "", `package httpapi
import ("context"; runner "retrom/internal/adapter/worker"; service "retrom/internal/service/libraryimport")
func New(ctx context.Context) { worker := &runner.Worker{Input:&service.Business{}}; worker.Start(ctx) }
`},
	)
}
