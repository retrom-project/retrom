package architecture

import (
	"strings"
	"testing"
)

const archiveFactsFixture = `package importing

type NestedArchiveFormat string
type ArchiveContent struct { Size int64; CRC32, MD5, SHA1, SHA256 string }
type ArchiveEntry struct {
	Ordinal int
	OriginalPath, NormalizedPath, ASCIICasefoldPath, ArchiveFormat, CompressionProfile string
	Size int64
	CRC32, MD5, SHA1, SHA256 string
	NestedArchive NestedArchiveFormat
}
type ArchiveMemberHeader struct { Entry ArchiveEntry; Unpacked bool }
`

const archiveModelFixture = `package libraryimport
import ("context"; facts "retrom/internal/capability/format/importing")
type StreamCursor interface {
	Next() (facts.ArchiveMemberHeader, error)
	Read(buffer []byte) (count int, err error)
	Complete(facts.ArchiveContent) (facts.ArchiveEntry, error)
	Close() error
}
type Archives interface { Open(context.Context) (StreamCursor, error) }
`

const archiveAdapterFixture = `package archive
import ("context"; "io"; facts "retrom/internal/capability/format/importing"; model "retrom/internal/model/libraryimport")
type Factory struct{}
func New() *Factory { return &Factory{} }
func (*Factory) Open(ctx context.Context) (model.StreamCursor, error) { return openCursor(ctx) }
func openCursor(context.Context) (model.StreamCursor, error) {
	cursor := &cursor{}
	return cursor, nil
}
type cursor struct{}
func (*cursor) Next() (facts.ArchiveMemberHeader, error) { return facts.ArchiveMemberHeader{}, io.EOF }
func (*cursor) Read(buffer []byte) (int, error) { return 0, io.EOF }
func (*cursor) Complete(content facts.ArchiveContent) (facts.ArchiveEntry, error) { return facts.ArchiveEntry{}, nil }
func (*cursor) Close() error { return nil }
`

const archiveServiceFixture = `package libraryimport
import ("context"; model "retrom/internal/model/libraryimport")
type Service struct { archives model.Archives }
func New(archives model.Archives) *Service { return &Service{archives: archives} }
func (service *Service) Run(ctx context.Context) error {
	reader, err := service.archives.Open(ctx)
	if err != nil { return err }
	defer reader.Close()
	_, err = reader.Next()
	return err
}
`

const archiveBootstrapFixture = `package composition
import ("retrom/internal/adapter/archive"; service "retrom/internal/service/libraryimport")
func Build() *service.Service { return service.New(archive.New()) }
`

const archiveMainFixture = `package main
import ("context"; "retrom/internal/bootstrap/composition")
func main() { service := composition.Build(); _ = service.Run(context.Background()) }
`

type archiveFixtureEdit struct {
	file, before, after string
}

func archiveFixtureSources() map[string]string {
	return map[string]string{
		"go.mod": "module retrom\n\ngo 1.26.5\n",
		"internal/capability/format/importing/facts.go": archiveFactsFixture,
		"internal/model/libraryimport/ports.go":         archiveModelFixture,
		"internal/adapter/archive/archive.go":           archiveAdapterFixture,
		"internal/service/libraryimport/service.go":     archiveServiceFixture,
		"internal/bootstrap/composition/creation.go":    archiveBootstrapFixture,
		"cmd/check/main.go":                             archiveMainFixture,
	}
}

func newArchiveFixture(t *testing.T, edits ...archiveFixtureEdit) (string, OwnershipRegistry) {
	t.Helper()
	sources := archiveFixtureSources()
	for _, edit := range edits {
		if edit.before == "" {
			sources[edit.file] = edit.after
			continue
		}
		old, exists := sources[edit.file]
		if !exists || !strings.Contains(old, edit.before) {
			t.Fatalf("fixture edit has no matching source: %+v", edit)
		}
		sources[edit.file] = strings.ReplaceAll(old, edit.before, edit.after)
	}
	root := newInventoryRepository(t)
	for name, source := range sources {
		writeInventoryFile(t, root, name, source)
	}
	return root, archiveFixtureOwners()
}

func archiveFixtureOwners() OwnershipRegistry {
	return OwnershipRegistry{Packages: []PackageOwnership{
		{Path: "internal/capability/format/importing", Layer: "capability", Module: "format/importing", Owner: "RF04"},
		{Path: "internal/model/libraryimport", Layer: "model", Module: "libraryimport", Owner: "RF04"},
		{Path: "internal/adapter/archive", Layer: "adapter", Module: "archive", Owner: "RF04"},
		{Path: "internal/service/libraryimport", Layer: "service", Module: "libraryimport", Owner: "RF13"},
		{Path: "internal/bootstrap/composition", Layer: "bootstrap", Module: "composition", Owner: "RF04"},
		{Path: "cmd/check", Layer: "cmd", Module: "main", Owner: "RF04"},
		{Path: "internal/repo/archive", Layer: "repo", Module: "archive", Owner: "RF04"},
		{Path: "internal/model/diagnostics", Layer: "model", Module: "diagnostics", Owner: "RF04"},
	}}
}

func inspectArchiveFixture(t *testing.T, root string, owners OwnershipRegistry, build string) ([]PortInventory, []Violation) {
	t.Helper()
	graph, err := loadInventoryGraph(t.Context(), root, []string{"./..."}, build)
	if err != nil {
		t.Fatal(err)
	}
	ports, violations := inspectPortGraph(root, graph, owners)
	labelArchiveBuild(ports, build)
	return ports, violations
}

func requireArchiveProof(t *testing.T, ports []PortInventory, violations []Violation) ArchiveResourceProof {
	t.Helper()
	if len(violations) != 0 {
		t.Fatalf("valid compiled binding rejected: %+v", violations)
	}
	port := findImplementationPort(t, ports, "(retrom/internal/model/libraryimport.Archives).Open")
	if len(port.ArchiveResources) != 1 {
		t.Fatalf("expected one generated resource proof: %+v", port)
	}
	proof := port.ArchiveResources[0]
	if proof.Status != "PROVEN" || len(proof.Bindings) == 0 || len(proof.Sources) < 6 {
		t.Fatalf("missing actual binding/source evidence: %+v", proof)
	}
	requireArchiveBindingEvidence(t, proof)
	return proof
}

func requireArchiveBindingEvidence(t *testing.T, proof ArchiveResourceProof) {
	t.Helper()
	for _, binding := range proof.Bindings {
		if binding.Consumer.Line < 1 || binding.Injection.Line < 1 || binding.Return.Line < 1 ||
			!strings.HasPrefix(binding.Injection.File, "internal/bootstrap/") ||
			!strings.HasPrefix(binding.Concrete, "*retrom/internal/adapter/archive.") || len(binding.Methods) != 4 {
			t.Fatalf("incomplete or non-production evidence: %+v", binding)
		}
	}
	for _, source := range proof.Sources {
		if len(source.SHA256) != 64 || source.Size == 0 || strings.HasSuffix(source.Path, "_test.go") {
			t.Fatalf("invalid production source evidence: %+v", source)
		}
	}
}

func requireArchiveRejected(t *testing.T, ports []PortInventory, violations []Violation, reason string) {
	t.Helper()
	for _, port := range ports {
		for _, proof := range port.ArchiveResources {
			if proof.Status == "PROVEN" {
				t.Fatalf("invalid binding received an exception: %+v", proof)
			}
		}
	}
	for _, violation := range violations {
		if violation.Rule == "AR03" && strings.Contains(violation.Message, reason) {
			if violation.Line < 1 || len(violation.DependencyChain) < 2 {
				t.Fatalf("rejection lost its source chain: %+v", violation)
			}
			return
		}
	}
	t.Fatalf("missing AR03 rejection %q: %+v", reason, violations)
}
