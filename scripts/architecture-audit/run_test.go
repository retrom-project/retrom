package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setupMinimalFixture(t *testing.T, dir string) {
	t.Helper()
	writeFixture(t, dir, "go.mod", "module retrom\ngo 1.23\n")
	for _, sub := range []string{"internal/model", "internal/service", "internal/repo"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStrictGateRejectsCallbackFixture(t *testing.T) {
	dir := t.TempDir()
	setupMinimalFixture(t, dir)
	writeFixture(t, dir, "internal/model/bad/ports.go",
		`package bad
import "context"
type Repository interface {
	WithWrite(context.Context, func(Scope) error) error
}
type Scope struct{}
`)

	var stdout, stderr bytes.Buffer
	code := Run(dir, "check", "", &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit 1 for callback fixture, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "LAYER-003") {
		t.Fatalf("expected LAYER-003 in stderr, got: %s", stderr.String())
	}
}

func TestStrictGatePassesCleanFixture(t *testing.T) {
	dir := t.TempDir()
	setupMinimalFixture(t, dir)
	writeFixture(t, dir, "internal/model/good/types.go",
		`package good
type Tag struct{ ID, Name string }
func Normalize(name string) string { return name }
`)

	var stdout, stderr bytes.Buffer
	code := Run(dir, "check", "", &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0 for clean fixture, got %d; stderr: %s", code, stderr.String())
	}
}

func TestInvalidModeReturnsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(t.TempDir(), "bogus", "", &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit 2 for invalid mode, got %d", code)
	}
}

func TestNonexistentRootReturnsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run("/nonexistent-path-12345", "check", "", &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit 2 for nonexistent root, got %d", code)
	}
}

func TestInventoryModeSucceeds(t *testing.T) {
	dir := t.TempDir()
	setupMinimalFixture(t, dir)
	writeFixture(t, dir, "internal/model/good/types.go",
		`package good
type Tag struct{ ID, Name string }
`)

	var stdout, stderr bytes.Buffer
	code := Run(dir, "inventory", "", &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0 for inventory, got %d; stderr: %s", code, stderr.String())
	}
}

func TestOutputFileWritten(t *testing.T) {
	dir := t.TempDir()
	setupMinimalFixture(t, dir)
	writeFixture(t, dir, "internal/model/good/types.go",
		`package good
type Tag struct{ ID, Name string }
`)

	outPath := filepath.Join(t.TempDir(), "report.json")
	var stdout, stderr bytes.Buffer
	code := Run(dir, "check", outPath, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("output file not written: %v", err)
	}
}
