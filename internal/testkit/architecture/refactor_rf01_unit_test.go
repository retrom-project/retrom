package architecture

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRefactorRF01_source_scope(t *testing.T) {
	t.Parallel()
	root := newInventoryRepository(t)
	writeInventoryFile(t, root, "internal/model/example/value.go", "package example\ntype Value struct { ID string }\n")
	runInventoryGit(t, root, "add", ".")
	sources, err := DiscoverSources(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	registry := OwnershipRegistry{
		SchemaVersion: 1,
		Baseline:      strings.Repeat("a", 40),
		Packages: []PackageOwnership{{
			Path: "internal/model/example", Layer: "model", Module: "example", Owner: "RF03",
			Files: []OwnedFile{{Path: "internal/model/example/value.go", Kind: "production"}},
		}},
	}
	if violations := ValidateOwnership(sources, registry); len(violations) != 0 {
		t.Fatalf("registered source rejected: %v", violations)
	}
	writeInventoryFile(t, root, "internal/model/helper/value.go", "package helper\n")
	sources, err = DiscoverSources(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	violations := ValidateOwnership(sources, registry)
	if len(violations) != 1 || violations[0].Rule != "GOV01" || violations[0].File != "internal/model/helper/value.go" {
		t.Fatalf("unregistered helper escaped ownership: %v", violations)
	}
}

func TestRefactorRF01_baseline_guard(t *testing.T) {
	t.Parallel()
	root := newInventoryRepository(t)
	writeInventoryFile(t, root, "source.go", "package fixture\n")
	runInventoryGit(t, root, "add", ".")
	runInventoryGit(t, root, "commit", "-qm", "baseline")
	baseline := runInventoryGit(t, root, "rev-parse", "HEAD")
	inspectInventoryBaseline(t, root, baseline, "BASELINE")
	writeInventoryFile(t, root, "source.go", "package fixture\nconst Revision = 2\n")
	runInventoryGit(t, root, "commit", "-qam", "descendant")
	inspectInventoryBaseline(t, root, baseline, "DESCENDANT")
	runInventoryGit(t, root, "checkout", "--orphan", "unrelated")
	runInventoryGit(t, root, "commit", "-qm", "unrelated root")
	before := runInventoryGit(t, root, "rev-parse", "HEAD")
	if _, err := InspectBaseline(t.Context(), root, baseline); err == nil {
		t.Fatal("unrelated commit passed baseline validation")
	}
	if got := runInventoryGit(t, root, "rev-parse", "HEAD"); got != before {
		t.Fatal("baseline validation mutated the checkout")
	}
}

func TestOwnershipRejectsStaleDuplicateAndUnsafeEntries(t *testing.T) {
	t.Parallel()
	entry := PackageOwnership{
		Path: "internal/model/example", Layer: "model", Module: "example", Owner: "RF03",
		Files: []OwnedFile{{Path: "internal/model/example/value.go", Kind: "production"}},
	}
	for _, test := range []struct {
		name     string
		sources  []string
		packages []PackageOwnership
	}{
		{name: "stale", sources: []string{"source.go"}, packages: []PackageOwnership{entry}},
		{name: "duplicate", sources: []string{entry.Files[0].Path}, packages: []PackageOwnership{entry, entry}},
		{name: "unsafe", sources: []string{entry.Files[0].Path}, packages: []PackageOwnership{{
			Path: "../outside", Layer: "model", Module: "example", Owner: "RF03",
			Files: []OwnedFile{{Path: "../outside/value.go", Kind: "production"}},
		}}},
		{name: "empty", sources: nil, packages: nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry := OwnershipRegistry{SchemaVersion: 1, Baseline: strings.Repeat("a", 40), Packages: test.packages}
			if violations := ValidateOwnership(test.sources, registry); len(violations) == 0 {
				t.Fatal("invalid ownership registry passed")
			}
		})
	}
}

func TestSourceFingerprintIncludesUntrackedBytesAndRejectsEscape(t *testing.T) {
	t.Parallel()
	root := newInventoryRepository(t)
	writeInventoryFile(t, root, "source.go", "package fixture\n")
	before, err := SnapshotFiles(root, []string{"source.go"})
	if err != nil {
		t.Fatal(err)
	}
	writeInventoryFile(t, root, "source.go", "package fixture\nconst Changed = true\n")
	after, err := SnapshotFiles(root, []string{"source.go"})
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(before, after) {
		t.Fatal("changed source bytes retained the same fingerprint")
	}
	if _, err := SnapshotFiles(root, []string{"../outside.go"}); err == nil {
		t.Fatal("unsafe source path accepted")
	}
	external := filepath.Join(t.TempDir(), "external.go")
	if err := os.WriteFile(external, []byte("package external\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	if _, err := SnapshotFiles(root, []string{"link.go"}); err == nil {
		t.Fatal("symlink source accepted")
	}
}

func TestSourceDiscoveryRejectsEmptyAndMissingFiles(t *testing.T) {
	t.Parallel()
	root := newInventoryRepository(t)
	if _, err := DiscoverSources(t.Context(), root); err == nil {
		t.Fatal("empty source discovery passed")
	}
	writeInventoryFile(t, root, "source.go", "package fixture\n")
	runInventoryGit(t, root, "add", ".")
	if err := os.Remove(filepath.Join(root, "source.go")); err != nil {
		t.Fatal(err)
	}
	sources, err := DiscoverSources(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SnapshotFiles(root, sources); err == nil {
		t.Fatal("missing tracked source passed fingerprinting")
	}
}

func newInventoryRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runInventoryGit(t, root, "init", "-q")
	runInventoryGit(t, root, "config", "user.email", "fixture@example.invalid")
	runInventoryGit(t, root, "config", "user.name", "Architecture fixture")
	runInventoryGit(t, root, "config", "commit.gpgsign", "false")
	return root
}

func writeInventoryFile(t *testing.T, root, relative, content string) {
	t.Helper()
	target := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runInventoryGit(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", arguments...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
	return strings.TrimSpace(string(output))
}

func inspectInventoryBaseline(t *testing.T, root, baseline, relation string) {
	t.Helper()
	report, err := InspectBaseline(t.Context(), root, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if report.Relation != relation || report.Baseline != baseline || report.Commit == "" || report.Tree == "" {
		t.Fatalf("incorrect baseline report: %+v", report)
	}
}
