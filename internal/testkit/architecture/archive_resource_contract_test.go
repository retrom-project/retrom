package architecture

import (
	"strings"
	"testing"
)

type archiveContractCase struct {
	name, declarations, reason string
	edit                       archiveFixtureEdit
}

func TestArchiveResourceRejectsContractEscapes(t *testing.T) {
	t.Parallel()
	for _, test := range archiveContractCases() {
		t.Run(test.name, func(t *testing.T) {
			root := newInventoryRepository(t)
			writeInventoryFile(t, root, "go.mod", "module retrom\n\ngo 1.26.5\n")
			facts := archiveFactsFixture
			declarations := test.declarations
			if test.edit.before != "" {
				facts = replaceArchiveFixture(t, facts, test.edit.before, test.edit.after)
				if test.name == "N15 callback hidden in facts" {
					facts += "\ntype Box[T any] struct { Value T }\n"
				}
				if test.name == "N15 context hidden in facts" {
					facts = replaceArchiveFixture(t, facts, "Unpacked bool", "Unpacked bool; Context context.Context")
				}
			}
			writeInventoryFile(t, root, "internal/capability/format/importing/facts.go", facts)
			writeInventoryFile(t, root, "internal/model/libraryimport/ports.go", declarations)
			ports, violations := inspectArchiveFixture(t, root, archiveFixtureOwners(), "default")
			requireArchiveRejected(t, ports, violations, test.reason)
		})
	}
}

func archiveContractCases() []archiveContractCase {
	return []archiveContractCase{
		{name: "N01 old session and member", reason: "capability interface", declarations: `package libraryimport
import facts "retrom/internal/capability/format/importing"
type Member interface { Read([]byte)(int,error); Header() facts.ArchiveEntry; Abort(error) error }
type Session interface { Next()(Member,error); Finish()([]facts.ArchiveEntry,error); Close()error }
type Archives interface { Open()(Session,error) }
`},
		{name: "N02 error input", reason: "exactly four", declarations: modifiedArchiveModel("Close() error", "Close() error; Abort(ErrorAlias) error") +
			"\ntype ErrorAlias = error\n"},
		{
			name: "N03 variadic read", reason: "noncanonical signature",
			declarations: modifiedArchiveModel("Read(buffer []byte)", "Read(buffer ...byte)"),
		},
		{
			name: "N03 wrong read result", reason: "noncanonical signature",
			declarations: modifiedArchiveModel("(count int, err error)", "(err error, count int)"),
		},
		{
			name: "N03 defined byte slice", reason: "noncanonical signature",
			declarations: modifiedArchiveModel("Read(buffer []byte)", "Read(buffer Bytes)") + "\ntype Bytes []byte\n",
		},
		{
			name: "N04 arbitrary result", reason: "noncanonical signature",
			declarations: modifiedArchiveModel("Next() (facts.ArchiveMemberHeader, error)", "Next() (any, error)"),
		},
		{
			name: "N04 callback complete", reason: "noncanonical signature",
			declarations: modifiedArchiveModel("Complete(facts.ArchiveContent)", "Complete(func())"),
		},
		{
			name: "N05 nested alias resource", reason: "capability interface",
			declarations: modifiedArchiveModel("(StreamCursor, error)", "(Box, error)") +
				"\ntype Alias = StreamCursor\ntype Box struct { Readers map[string][]*Alias }\n",
		},
		{
			name: "N05 generic container", reason: "capability interface",
			declarations: modifiedArchiveModel("(StreamCursor, error)", "(Box[StreamCursor], error)") +
				"\ntype Box[T any] struct { Value T }\n",
		},
		{
			name: "N06 resource input", reason: "capability interface",
			declarations: archiveModelFixture + "\ntype Input interface { Use(StreamCursor) error }\n",
		},
		{
			name: "N06 close-only input", reason: "capability interface",
			declarations: archiveModelFixture + "\ntype Lease interface { Close() error }\ntype Input interface { Use(Lease) error }\n",
		},
		{name: "N07 generic resource", reason: "generic archive", declarations: genericArchiveModel(false)},
		{name: "N07 generic alias", reason: "generic archive", declarations: genericArchiveModel(true)},
		{
			name: "N08 embedded methods", reason: "exactly four",
			declarations: modifiedArchiveModel("Read(buffer []byte) (count int, err error)", "ReadPart") +
				"\ntype ReadPart interface { Read([]byte)(int,error) }\n",
		},
		{
			name: "N08 extra business method", reason: "exactly four",
			declarations: modifiedArchiveModel("Close() error", "Close() error; Publish() error"),
		},
		{
			name: "N15 callback hidden in facts", reason: "recursively closed",
			declarations: archiveModelFixture, edit: archiveFixtureEdit{
				before: "Unpacked bool", after: "Unpacked bool; Hidden map[string][]*Box[func()]",
			},
		},
		{
			name: "N15 context hidden in facts", reason: "recursively closed",
			declarations: archiveModelFixture, edit: archiveFixtureEdit{
				before: "package importing", after: "package importing\nimport \"context\"\n",
			},
		},
	}
}

func modifiedArchiveModel(before, after string) string {
	return strings.ReplaceAll(archiveModelFixture, before, after)
}

func genericArchiveModel(alias bool) string {
	if alias {
		return modifiedArchiveModel("(StreamCursor, error)", "(Alias[func()], error)") +
			"\ntype Alias[T any] = StreamCursor\n"
	}
	return strings.ReplaceAll(
		modifiedArchiveModel("type StreamCursor interface", "type StreamCursor[T any] interface"),
		"(StreamCursor, error)", "(StreamCursor[func()], error)",
	)
}

func replaceArchiveFixture(t *testing.T, source, before, after string) string {
	t.Helper()
	if !strings.Contains(source, before) {
		t.Fatalf("fixture edit has no match: %s", before)
	}
	return strings.ReplaceAll(source, before, after)
}
