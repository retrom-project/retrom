package architecture

import (
	"strings"
	"testing"
)

type archiveBindingCase struct {
	name, reason string
	edits        []archiveFixtureEdit
}

func TestArchiveResourceRejectsUnprovedProductionOrigins(t *testing.T) {
	t.Parallel()
	for _, test := range archiveBindingCases() {
		t.Run(test.name, func(t *testing.T) {
			root, owners := newArchiveFixture(t, test.edits...)
			ports, violations := inspectArchiveFixture(t, root, owners, "default")
			requireArchiveRejected(t, ports, violations, test.reason)
		})
	}
}

func archiveBindingCases() []archiveBindingCase {
	return []archiveBindingCase{
		{
			name: "N09 hidden concrete business method", reason: "hidden extra resource method",
			edits: []archiveFixtureEdit{{"internal/adapter/archive/archive.go", "", archiveAdapterFixture +
				"\nfunc (*cursor) Publish() error { return nil }\n"}},
		},
		{
			name: "N10 promoted Service implementation", reason: "forbidden layer: service",
			edits: promotedArchiveFixture(),
		},
		{
			name: "N10 Model business interface injection", reason: "unapproved interface",
			edits: []archiveFixtureEdit{
				{"internal/model/libraryimport/ports.go", "", archiveModelFixture + "\ntype Policy interface { Apply() error }\n"},
				{"internal/adapter/archive/archive.go", "type Factory struct{}", "type Factory struct { policy model.Policy }"},
			},
		},
		{
			name: "N10 unused callback constructor parameter", reason: "callback",
			edits: []archiveFixtureEdit{
				{"internal/adapter/archive/archive.go", "func New()", "func New(callback func())"},
				{"internal/bootstrap/composition/creation.go", "archive.New()", "archive.New(nil)"},
			},
		},
		{
			name: "N11 Repo acquisition", reason: "capability interface",
			edits: []archiveFixtureEdit{{"internal/repo/archive/repo.go", "", `package archive
import ("context"; model "retrom/internal/model/libraryimport")
type Repository struct{}
func (*Repository) Open(context.Context) (model.StreamCursor,error) { return nil,nil }
`}},
		},
		{
			name: "N12 returned Repo resource", reason: "unresolved archive",
			edits: returnedRepoArchiveFixture(),
		},
		{
			name: "N13 compile assertion without acquisition", reason: "no actual production acquisition",
			edits: []archiveFixtureEdit{
				{"internal/adapter/archive/archive.go", "", archiveAdapterFixture + "\nvar _ model.Archives = (*Factory)(nil)\n"},
				{"internal/service/libraryimport/service.go", "", `package libraryimport
import model "retrom/internal/model/libraryimport"
type Service struct { archives model.Archives }
func New(archives model.Archives) *Service { return &Service{archives:archives} }
`},
				{"cmd/check/main.go", "", `package main
import "retrom/internal/bootstrap/composition"
func main() { _ = composition.Build() }
`},
			},
		},
		{
			name: "N13 unused factory and composition", reason: "no reachable Bootstrap injection",
			edits: []archiveFixtureEdit{{"cmd/check/main.go", "", `package main
import "retrom/internal/bootstrap/composition"
func main() { _ = composition.Build }
`}},
		},
		{
			name: "N13 test-only factory cannot bind production", reason: "unresolved archive",
			edits: testOnlyArchiveFixture(),
		},
		{
			name: "N14 dynamic return", reason: "dynamic constructor or factory callback",
			edits: []archiveFixtureEdit{{
				"internal/adapter/archive/archive.go", "return openCursor(ctx)",
				"var makeCursor func() model.StreamCursor; return makeCursor(), nil",
			}},
		},
		{
			name: "N14 unbound global return", reason: "unbound",
			edits: []archiveFixtureEdit{
				{"internal/adapter/archive/archive.go", "type Factory struct{}", "var shared model.StreamCursor\ntype Factory struct{}"},
				{"internal/adapter/archive/archive.go", "return openCursor(ctx)", "return shared, nil"},
			},
		},
		{
			name: "N14 reassigned return", reason: "reassigned",
			edits: []archiveFixtureEdit{
				{
					"internal/adapter/archive/archive.go", "return cursor, nil",
					"cursor = &cursorReplacement{}; return cursor, nil",
				},
				{"internal/adapter/archive/archive.go", "cursor := &cursor{}", "var cursor model.StreamCursor = &cursor{}"},
				{"internal/adapter/archive/archive.go", "type cursor struct{}", "type cursor struct{}\ntype cursorReplacement struct { cursor }"},
			},
		},
		{
			name: "N14 named return", reason: "named or incomplete return",
			edits: []archiveFixtureEdit{{
				"internal/adapter/archive/archive.go",
				"func openCursor(context.Context) (model.StreamCursor, error) {\n\tcursor := &cursor{}\n\treturn cursor, nil\n}",
				"func openCursor(context.Context) (value model.StreamCursor, err error) { value = &cursor{}; return }",
			}},
		},
		{
			name: "N14 nil successful return", reason: "nil resource without a proven failure",
			edits: []archiveFixtureEdit{{"internal/adapter/archive/archive.go", "return openCursor(ctx)", "return nil, nil"}},
		},
		{
			name: "N15 business execution through Adapter", reason: "business or construction layer",
			edits: []archiveFixtureEdit{
				{
					"internal/adapter/archive/archive.go", `import ("context";`,
					`import ("retrom/internal/service/libraryimport"; "context";`,
				},
				{"internal/adapter/archive/archive.go", "return openCursor(ctx)", "libraryimport.Execute(); return openCursor(ctx)"},
				{"internal/service/libraryimport/business.go", "", "package libraryimport\nfunc Execute() {}\n"},
			},
		},
		{
			name: "N16 Service constructs Adapter", reason: "construction outside Bootstrap",
			edits: []archiveFixtureEdit{
				{
					"internal/service/libraryimport/service.go", `import ("context";`,
					`import ("retrom/internal/adapter/archive"; "context";`,
				},
				{"internal/service/libraryimport/service.go", "&Service{archives: archives}", "&Service{archives: archive.New()}"},
			},
		},
		{
			name: "mutation invalidates constructor proof", reason: "injection field is mutated",
			edits: []archiveFixtureEdit{{"internal/service/libraryimport/service.go", "", archiveServiceFixture +
				"\nfunc (service *Service) Replace(value model.Archives) { service.archives = value }\n"}},
		},
	}
}

func promotedArchiveFixture() []archiveFixtureEdit {
	split := strings.Index(archiveAdapterFixture, "type cursor struct{}")
	adapter := archiveAdapterFixture[:split] + "type cursor struct { *libraryimport.Cursor }\n"
	adapter = strings.ReplaceAll(adapter, `"io"; facts "retrom/internal/capability/format/importing";`,
		`"retrom/internal/service/libraryimport";`)
	service := "package libraryimport\nimport (\"io\"; facts \"retrom/internal/capability/format/importing\")\n" +
		strings.ReplaceAll(archiveAdapterFixture[split:], "cursor", "Cursor")
	return []archiveFixtureEdit{
		{"internal/adapter/archive/archive.go", "", adapter},
		{"internal/service/libraryimport/cursor.go", "", service},
	}
}

func returnedRepoArchiveFixture() []archiveFixtureEdit {
	split := strings.Index(archiveAdapterFixture, "type cursor struct{}")
	repository := "package archive\nimport (\"io\"; facts \"retrom/internal/capability/format/importing\")\n" +
		strings.ReplaceAll(archiveAdapterFixture[split:], "cursor", "Cursor")
	return []archiveFixtureEdit{
		{"internal/adapter/archive/archive.go", "", `package archive
import ("context"; model "retrom/internal/model/libraryimport"; "retrom/internal/repo/archive")
type Factory struct{}
func New() *Factory { return &Factory{} }
func (*Factory) Open(context.Context) (model.StreamCursor,error) { return &archive.Cursor{},nil }
`},
		{"internal/repo/archive/cursor.go", "", repository},
	}
}

func testOnlyArchiveFixture() []archiveFixtureEdit {
	return []archiveFixtureEdit{
		{"internal/adapter/archive/archive.go", "", "package archive\n"},
		{"internal/adapter/archive/archive_test.go", "", archiveAdapterFixture},
		{"internal/bootstrap/composition/creation.go", "", `package composition
import service "retrom/internal/service/libraryimport"
func Build() *service.Service { return service.New(nil) }
`},
	}
}
