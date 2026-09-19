package architecture

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompatibilityRejectsChangedMissingAndNewContractInputs(t *testing.T) {
	t.Parallel()
	root := newInventoryRepository(t)
	writeInventoryFile(t, root, "api/openapi.yaml", "openapi: 3.0.3\n")
	writeInventoryFile(t, root, "migrations/001_schema.sql", "CREATE TABLE examples (id TEXT);\n")
	writeInventoryFile(t, root, "testdata/public-roms/example/fixture.bin", "owned public fixture")
	runInventoryGit(t, root, "add", ".")
	runInventoryGit(t, root, "commit", "-qm", "baseline")
	baseline := runInventoryGit(t, root, "rev-parse", "HEAD")
	report, err := InspectCompatibility(t.Context(), root, baseline)
	if err != nil || len(report.Violations) != 0 || len(report.Files) != 3 {
		t.Fatalf("unchanged inputs rejected: %+v, %v", report, err)
	}
	writeInventoryFile(t, root, "api/openapi.yaml", "openapi: 3.1.0\n")
	writeInventoryFile(t, root, "migrations/002_added.sql", "SELECT 1;\n")
	if err := os.Remove(filepath.Join(root, "testdata/public-roms/example/fixture.bin")); err != nil {
		t.Fatal(err)
	}
	report, err = InspectCompatibility(t.Context(), root, baseline)
	if err != nil || len(report.Violations) != 3 {
		t.Fatalf("compatibility changes were missed: %+v, %v", report, err)
	}
	for _, violation := range report.Violations {
		if violation.Rule != "AR11" {
			t.Fatalf("wrong rule: %+v", violation)
		}
	}
}

func TestSyntaxInventoryIncludesExcludedBuildFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeInventoryFile(t, root, "hidden.go", "//go:build unknown_architecture_tag\n\npackage fixture\nvar Missing = }\n")
	if _, err := InspectGoSyntax(root, []string{"hidden.go"}); err == nil {
		t.Fatal("malformed source hidden by a build tag escaped the syntax channel")
	}
}
