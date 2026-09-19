package architecture

import (
	"strings"
	"testing"
)

func TestArchiveResourceRejectsBusinessObjectsDisguisedAsStreams(t *testing.T) {
	t.Parallel()
	for _, lease := range []bool{false, true} {
		name := "reader"
		if lease {
			name = "lease"
		}
		t.Run(name, func(t *testing.T) {
			root, owners := newArchiveFixture(t, injectedArchiveFixture(lease)...)
			ports, violations := inspectArchiveFixture(t, root, owners, "default")
			requireArchiveRejected(t, ports, violations, "external resource injection")
		})
	}
}

func injectedArchiveFixture(lease bool) []archiveFixtureEdit {
	inputType := "io.Reader"
	business := "func (*Business) Read([]byte)(int,error) { return 0,nil }\n"
	model := archiveModelFixture
	if lease {
		inputType = "model.Lease"
		business = "func (*Business) Close() error { return nil }\n"
		model += "\ntype Lease interface { Close() error }\n"
	}
	adapter := strings.ReplaceAll(archiveAdapterFixture, "type Factory struct{}",
		"type Factory struct { input "+inputType+" }")
	adapter = strings.ReplaceAll(adapter, "func New() *Factory { return &Factory{} }",
		"func New(input "+inputType+") *Factory { return &Factory{input:input} }")
	adapter = strings.ReplaceAll(adapter, "func (*Factory) Open(ctx context.Context)",
		"func (factory *Factory) Open(ctx context.Context)")
	adapter = strings.ReplaceAll(adapter, "return openCursor(ctx)", "return &cursor{input:factory.input},nil")
	adapter = strings.ReplaceAll(adapter, "type cursor struct{}", "type cursor struct { input "+inputType+" }")
	if lease {
		adapter = strings.ReplaceAll(adapter, "func (*cursor) Close() error { return nil }",
			"func (value *cursor) Close() error { return value.input.Close() }")
	} else {
		adapter = strings.ReplaceAll(adapter, "func (*cursor) Read(buffer []byte) (int, error) { return 0, io.EOF }",
			"func (value *cursor) Read(buffer []byte) (int, error) { return value.input.Read(buffer) }")
	}
	return []archiveFixtureEdit{
		{"internal/model/libraryimport/ports.go", "", model},
		{"internal/adapter/archive/archive.go", "", adapter},
		{"internal/service/libraryimport/business.go", "", "package libraryimport\ntype Business struct{}\n" + business},
		{"internal/bootstrap/composition/creation.go", "archive.New()", "archive.New(&service.Business{})"},
	}
}

func TestArchiveResourceRecognizesRealCommandEntry(t *testing.T) {
	t.Parallel()
	root, owners := newArchiveFixture(t)
	for index := range owners.Packages {
		if owners.Packages[index].Path == "cmd/check" {
			owners.Packages[index].Layer = "cmd"
		}
	}
	ports, violations := inspectArchiveFixture(t, root, owners, "default")
	requireArchiveProof(t, ports, violations)
}

func TestArchiveResourceRequiresCanonicalEnumDefinition(t *testing.T) {
	t.Parallel()
	for _, declaration := range []string{"var NestedArchiveFormat string", "type NestedArchiveFormat = string"} {
		t.Run(declaration, func(t *testing.T) {
			root, owners := newArchiveFixture(t,
				archiveFixtureEdit{"internal/capability/format/importing/facts.go", "type NestedArchiveFormat string", declaration},
				archiveFixtureEdit{"internal/capability/format/importing/facts.go", "NestedArchive NestedArchiveFormat", "NestedArchive string"},
			)
			ports, violations := inspectArchiveFixture(t, root, owners, "default")
			requireArchiveRejected(t, ports, violations, "recursively closed")
		})
	}
}

func TestArchiveResourceRejectsCapturedInjectionParameter(t *testing.T) {
	t.Parallel()
	root, owners := newArchiveFixture(t, archiveFixtureEdit{
		"internal/service/libraryimport/service.go",
		"return &Service{archives: archives}",
		"func(){ archives = nil }(); return &Service{archives: archives}",
	})
	ports, violations := inspectArchiveFixture(t, root, owners, "default")
	requireArchiveRejected(t, ports, violations, "reassigned")
}

func TestArchiveResourceRejectsWholeInjectionReplacement(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, mutation, helper, reason string }{
		{"direct", "*box = options{Factory: service.Replacement{}}", "", "reassigned"},
		{"local-alias", "alias := box; *alias = options{Factory: service.Replacement{}}", "", "field is mutated"},
		{"helper-alias", "replace(box)", "func replace(alias *options) { *alias = options{Factory: service.Replacement{}} }", "field is mutated"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			bootstrap := `package composition
import ("retrom/internal/adapter/archive"; model "retrom/internal/model/libraryimport"; service "retrom/internal/service/libraryimport")
type options struct { Factory model.Archives }
func Build() *service.Service {
 box := &options{Factory: archive.New()}
 ` + test.mutation + `
 return service.New(box.Factory)
}
` + test.helper
			root, owners := newArchiveFixture(t,
				archiveFixtureEdit{"internal/bootstrap/composition/creation.go", "", bootstrap},
				archiveFixtureEdit{"internal/service/libraryimport/business.go", "", `package libraryimport
import ("context"; "errors"; model "retrom/internal/model/libraryimport")
type Replacement struct{}
func (Replacement) Open(context.Context) (model.StreamCursor,error) { return nil,errors.New("business factory") }
`},
			)
			ports, violations := inspectArchiveFixture(t, root, owners, "default")
			requireArchiveRejected(t, ports, violations, test.reason)
		})
	}
}

func TestArchiveResourceRejectsBorrowedAcquireReader(t *testing.T) {
	t.Parallel()
	root, owners := newArchiveFixture(t, archiveAcquireReaderFixture()...)
	ports, violations := inspectArchiveFixture(t, root, owners, "default")
	requireArchiveRejected(t, ports, violations, "inputs after context")
}

func archiveAcquireReaderFixture() []archiveFixtureEdit {
	model := strings.ReplaceAll(archiveModelFixture, `"context";`, `"context"; "io";`)
	model = strings.ReplaceAll(model, "Open(context.Context)", "Open(context.Context, io.Reader)")
	adapter := strings.ReplaceAll(archiveAdapterFixture, "Open(ctx context.Context)", "Open(ctx context.Context, input io.Reader)")
	adapter = strings.ReplaceAll(adapter, "return openCursor(ctx)", "return openCursor(ctx, input)")
	adapter = strings.ReplaceAll(adapter, "func openCursor(context.Context)", "func openCursor(_ context.Context, input io.Reader)")
	adapter = strings.ReplaceAll(adapter, "cursor := &cursor{}", "cursor := &cursor{input:input}")
	adapter = strings.ReplaceAll(adapter, "type cursor struct{}", "type cursor struct { input io.Reader }")
	adapter = strings.ReplaceAll(adapter,
		"func (*cursor) Next() (facts.ArchiveMemberHeader, error) { return facts.ArchiveMemberHeader{}, io.EOF }",
		"func (value *cursor) Next() (facts.ArchiveMemberHeader, error) { _, err := value.input.Read(make([]byte,1)); return facts.ArchiveMemberHeader{}, err }")
	service := strings.ReplaceAll(archiveServiceFixture, "service.archives.Open(ctx)", "service.archives.Open(ctx, service)")
	return []archiveFixtureEdit{
		{"internal/model/libraryimport/ports.go", "", model},
		{"internal/adapter/archive/archive.go", "", adapter},
		{"internal/service/libraryimport/service.go", "", service},
		{"internal/service/libraryimport/business.go", "", `package libraryimport
import "io"
func (*Service) Read([]byte) (int,error) { return 0,io.EOF }
`},
	}
}

func TestArchiveResourceRejectsExternalFactoryResourceFields(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, construction string }{
		{"composite", "factory := &archive.Factory{Input: &service.Business{}}"},
		{"field-write", "factory := archive.New(); factory.Input = &service.Business{}"},
		{"whole-write", "factory := archive.New(); *factory = archive.Factory{Input: &service.Business{}}"},
		{"side-effect-helper", "factory := archive.New(); archive.Inject(factory, &service.Business{})"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			edits := injectedArchiveFixture(false)
			for index := range edits {
				if edits[index].file == "internal/adapter/archive/archive.go" {
					edits[index].after = strings.ReplaceAll(edits[index].after, "input", "Input")
					edits[index].after = strings.ReplaceAll(edits[index].after,
						"func New(Input io.Reader) *Factory { return &Factory{Input:Input} }",
						"func New() *Factory { return &Factory{} }")
					edits[index].after += "\nfunc Inject(factory *Factory, input io.Reader) { factory.Input = input }\n"
				}
			}
			edits[len(edits)-1] = archiveFixtureEdit{"internal/bootstrap/composition/creation.go", "", `package composition
import ("retrom/internal/adapter/archive"; service "retrom/internal/service/libraryimport")
func Build() *service.Service { ` + test.construction + `; return service.New(factory) }
`}
			root, owners := newArchiveFixture(t, edits...)
			ports, violations := inspectArchiveFixture(t, root, owners, "default")
			requireArchiveRejected(t, ports, violations, "")
		})
	}
}

func TestArchiveResourceRejectsDirectCommandConstruction(t *testing.T) {
	t.Parallel()
	root, owners := newArchiveFixture(t, archiveFixtureEdit{"cmd/check/main.go", "", `package main
import ("context"; service "retrom/internal/service/libraryimport"; "retrom/internal/adapter/archive")
func main() { _ = service.New(archive.New()).Run(context.Background()) }
`})
	ports, violations := inspectArchiveFixture(t, root, owners, "default")
	requireArchiveRejected(t, ports, violations, "constructed outside Bootstrap")
}
