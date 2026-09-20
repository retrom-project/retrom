package architecture

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb/dirhash"
	modzip "golang.org/x/mod/zip"
)

const archiveExternalModulePath = "retrom.test/archivecodec"

const archiveExternalModuleFixture = `package codec

type Transformer interface { Transform(string) (string,error) }
type Codec interface { NewDecoder() *Decoder }
type Decoder struct { Transformer Transformer }
func (decoder *Decoder) String(value string)(string,error){return decoder.Transformer.Transform(value)}
type Config struct { Transformer Transformer }
func (config *Config) NewDecoder()*Decoder{return &Decoder{Transformer:config.Transformer}}
type identity struct{}
func(identity) Transform(value string)(string,error){return value,nil}
var configuration = Config{Transformer:identity{}}
var Legacy Codec = &configuration
var All = []Codec{Legacy}
`

const archiveExternalCallFixture = `package zipentry
import codec "retrom.test/archivecodec"
func DecodeName(value string, nonUTF8 bool)(string,error){return codec.Legacy.NewDecoder().String(value)}
`

func archiveExternalConsumerEdits() []archiveFixtureEdit {
	return []archiveFixtureEdit{
		{"internal/capability/format/zipentry/name.go", "", archiveExternalCallFixture},
		{"internal/adapter/archive/archive.go", `"context";`, `"context"; "retrom/internal/capability/format/zipentry";`},
		{"internal/adapter/archive/archive.go", "cursor := &cursor{}", "name, err := zipentry.DecodeName(\"member\", true)\n\tif err != nil { return nil, err }\n\tcursor := &cursor{name:name}"},
		{"internal/adapter/archive/archive.go", "type cursor struct{}", "type cursor struct{name string}"},
		{"internal/adapter/archive/archive.go", "func (*cursor) Next() (facts.ArchiveMemberHeader, error) { return facts.ArchiveMemberHeader{}, io.EOF }", "func (cursor *cursor) Next() (facts.ArchiveMemberHeader, error) { return facts.ArchiveMemberHeader{Entry:facts.ArchiveEntry{NormalizedPath:cursor.name}}, nil }"},
	}
}

func archiveExternalOwners(owners OwnershipRegistry) OwnershipRegistry {
	owners.Packages = append(owners.Packages, PackageOwnership{
		Path: "internal/capability/format/zipentry", Layer: "capability", Module: "format/zipentry", Owner: "RF04",
	})
	return owners
}

func archiveExternalModule(t *testing.T, source string) (string, string) {
	t.Helper()
	stage := t.TempDir()
	moduleRoot := filepath.Join(stage, "module")
	if err := os.MkdirAll(moduleRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	moduleText := "module " + archiveExternalModulePath + "\n\ngo 1.26.5\n"
	writeInventoryFile(t, moduleRoot, "go.mod", moduleText)
	writeInventoryFile(t, moduleRoot, "codec.go", source)
	version := module.Version{Path: archiveExternalModulePath, Version: "v1.0.0"}
	proxy := filepath.Join(stage, "proxy")
	entry := filepath.Join(proxy, archiveExternalModulePath, "@v")
	if err := os.MkdirAll(entry, 0o700); err != nil {
		t.Fatal(err)
	}
	archive, err := os.Create(filepath.Join(entry, "v1.0.0.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if err := modzip.CreateFromDir(archive, version, moduleRoot); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	writeInventoryFile(t, entry, "v1.0.0.mod", moduleText)
	writeInventoryFile(t, entry, "v1.0.0.info", `{"Version":"v1.0.0","Time":"2000-01-01T00:00:00Z"}`)
	sum, err := dirhash.HashDir(moduleRoot, version.String(), dirhash.Hash1)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOMODCACHE", filepath.Join(stage, "cache"))
	t.Setenv("GOPROXY", "file://"+filepath.ToSlash(proxy))
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOFLAGS", strings.TrimSpace(os.Getenv("GOFLAGS")+" -modcacherw"))
	return "module retrom\n\ngo 1.26.5\n\nrequire " + version.Path + " " + version.Version + "\n", version.Path + " " + version.Version + " " + sum + "\n"
}

func archiveExternalFixedFixture(t *testing.T, source string, edits ...archiveFixtureEdit) (string, OwnershipRegistry) {
	t.Helper()
	mod, sum := archiveExternalModule(t, source)
	base := archiveExternalConsumerEdits()
	base = append(base, archiveFixtureEdit{"go.mod", "", mod}, archiveFixtureEdit{"go.sum", "", sum})
	base = append(base, edits...)
	root, owners := newArchiveFixture(t, base...)
	command := exec.CommandContext(t.Context(), "go", "mod", "download")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("download fixed local module: %v\n%s", err, output)
	}
	return root, archiveExternalOwners(owners)
}

func archiveExternalRuntime(t *testing.T, root, assertion string) {
	t.Helper()
	source := `package main
import("context";"testing";"retrom/internal/bootstrap/composition")
func TestActualBinding(t *testing.T){` + assertion + `}
`
	writeInventoryFile(t, root, "cmd/check/binding_test.go", source)
	command := exec.CommandContext(t.Context(), "go", "test", "-count=1", "./...")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("actual external binding: %v\n%s", err, output)
	}
}

func archiveExternalRealCodec(t *testing.T, edits ...archiveFixtureEdit) (string, OwnershipRegistry) {
	t.Helper()
	repository, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	name, err := os.ReadFile(filepath.Join(repository, "internal/capability/format/zipentry/name.go"))
	if err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(repository, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{}
	for _, line := range strings.Split(string(sum), "\n") {
		if strings.HasPrefix(line, "golang.org/x/text v0.40.0 ") || strings.HasPrefix(line, "golang.org/x/text v0.40.0/go.mod ") {
			lines = append(lines, line)
		}
	}
	if len(lines) != 2 {
		t.Fatal("fixed real codec dependency unavailable")
	}
	base := archiveExternalConsumerEdits()
	base = append(base,
		archiveFixtureEdit{"internal/capability/format/zipentry/name.go", "", string(name)},
		archiveFixtureEdit{"internal/adapter/archive/archive.go", `DecodeName("member", true)`, `DecodeName("\xd6\xd0", true)`},
		archiveFixtureEdit{"go.mod", "", "module retrom\n\ngo 1.26.5\n\nrequire golang.org/x/text v0.40.0\n"},
		archiveFixtureEdit{"go.sum", "", strings.Join(lines, "\n") + "\n"},
	)
	root, owners := newArchiveFixture(t, append(base, edits...)...)
	return root, archiveExternalOwners(owners)
}

func archiveExternalRecord(t *testing.T, name string, ports []PortInventory, issues []Violation) {
	t.Helper()
	directory := os.Getenv("RETROM_ARCHIVE_EXTERNAL_EVIDENCE")
	if directory == "" {
		return
	}
	data, err := json.MarshalIndent(map[string]any{"ports": ports, "violations": issues}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, name+".json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
