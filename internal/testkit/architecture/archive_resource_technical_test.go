package architecture

import (
	"strings"
	"testing"
)

func TestArchiveResourceRejectsOpaqueResourceSources(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"global-reader", "context-value"} {
		t.Run(mode, func(t *testing.T) {
			root, owners := newArchiveFixture(t, opaqueArchiveFixture(mode)...)
			ports, violations := inspectArchiveFixture(t, root, owners, "default")
			reason := "package-level executable or resource state"
			if mode == "context-value" {
				reason = "opaque context data"
			}
			requireArchiveRejected(t, ports, violations, reason)
		})
	}
}

func opaqueArchiveFixture(mode string) []archiveFixtureEdit {
	adapter := strings.ReplaceAll(archiveAdapterFixture, "return openCursor(ctx)", "return &cursor{input: SOURCE},nil")
	adapter = strings.ReplaceAll(adapter, "type cursor struct{}", "type cursor struct{ input io.Reader }")
	adapter = strings.ReplaceAll(adapter,
		"func (*cursor) Next() (facts.ArchiveMemberHeader, error) { return facts.ArchiveMemberHeader{}, io.EOF }",
		"func (c *cursor) Next() (facts.ArchiveMemberHeader,error) { _,err:=c.input.Read(make([]byte,1)); return facts.ArchiveMemberHeader{},err }")
	service, bootstrap := archiveServiceFixture, archiveBootstrapFixture
	if mode == "global-reader" {
		adapter = strings.ReplaceAll(adapter, "SOURCE", "shared")
		adapter += "\nvar shared io.Reader\nfunc Configure(input io.Reader) { shared=input }\n"
		bootstrap = strings.ReplaceAll(bootstrap, "return service.New(archive.New())",
			"archive.Configure(&service.Service{}); return service.New(archive.New())")
	} else {
		adapter = strings.ReplaceAll(adapter, "SOURCE", "ctx.Value(\"reader\").(io.Reader)")
		service = strings.ReplaceAll(service, "service.archives.Open(ctx)",
			"service.archives.Open(context.WithValue(ctx,\"reader\",service))")
	}
	service += "\nfunc (*Service) Read([]byte)(int,error) { return 0,nil }\n"
	return []archiveFixtureEdit{
		{"internal/adapter/archive/archive.go", "", adapter},
		{"internal/service/libraryimport/service.go", "", service},
		{"internal/bootstrap/composition/creation.go", "", bootstrap},
	}
}

func TestArchiveResourceAllowsOwnedTechnicalState(t *testing.T) {
	t.Parallel()
	adapter := strings.ReplaceAll(archiveAdapterFixture,
		`"context"; "io";`, `"context"; "io"; "os"; diag "retrom/internal/model/diagnostics";`)
	adapter = strings.ReplaceAll(adapter, "type Factory struct{}", "type Factory struct { reporter diag.ErrorReporter }")
	adapter = strings.ReplaceAll(adapter, "func New() *Factory { return &Factory{} }",
		"func New(reporter diag.ErrorReporter) *Factory { return &Factory{reporter:reporter} }")
	adapter = strings.ReplaceAll(adapter, "func (*Factory) Open(ctx context.Context)",
		"func (factory *Factory) Open(ctx context.Context)")
	adapter = strings.ReplaceAll(adapter, "return openCursor(ctx)", "return &cursor{ctx:ctx, reporter:factory.reporter}, nil")
	adapter = strings.ReplaceAll(adapter, "type cursor struct{}",
		"type cursor struct { ctx context.Context; reader io.Reader; file *os.File; reporter diag.ErrorReporter }")
	adapter = strings.ReplaceAll(adapter, "func (*cursor) Close() error { return nil }",
		"func (value *cursor) Close() error { value.reporter.Report(value.ctx, diag.DiagnosticEvent{}); return nil }")
	adapter += "\ntype Reporter struct{}\nfunc (*Reporter) Report(context.Context,diag.DiagnosticEvent) {}\n"
	root, owners := newArchiveFixture(t,
		archiveFixtureEdit{"internal/model/diagnostics/events.go", "", `package diagnostics
import "context"
type DiagnosticEvent struct { Operation, Code, Message, RequestID string }
type ErrorReporter interface { Report(context.Context,DiagnosticEvent) }
`},
		archiveFixtureEdit{"internal/adapter/archive/archive.go", "", adapter},
		archiveFixtureEdit{"internal/bootstrap/composition/creation.go", "archive.New()", "archive.New(&archive.Reporter{})"},
	)
	ports, violations := inspectArchiveFixture(t, root, owners, "default")
	proof := requireArchiveProof(t, ports, violations)
	for _, source := range proof.Sources {
		if source.Path == "internal/model/diagnostics/events.go" {
			return
		}
	}
	t.Fatal("fixed diagnostic contract missing from source evidence")
}
