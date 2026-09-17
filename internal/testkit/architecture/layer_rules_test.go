package architecture

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for dir != "/" {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Skip("cannot locate repository root")
	return ""
}

// --- Real production scans ---

func TestLayeringNoModelOrRepoServiceDependency(t *testing.T) {
	root := repoRoot(t)
	result, err := ScanDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range result.Violations {
		if v.Rule == RuleModelRepoNoServiceDep {
			t.Errorf("[%s] %s:%d %s", v.Rule, v.File, v.Line, v.Message)
		}
	}
}

func TestLayeringNoServiceModelReexport(t *testing.T) {
	root := repoRoot(t)
	result, err := ScanDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range result.Violations {
		if v.Rule == RuleServiceNoModelReexport {
			t.Errorf("[%s] %s:%d %s (%s)", v.Rule, v.File, v.Line, v.Message, v.Symbol)
		}
	}
}

func TestLayeringModelPurity(t *testing.T) {
	root := repoRoot(t)
	result, err := ScanDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range result.Violations {
		if v.Rule == RuleModelNoPureViolation {
			t.Errorf("[%s] %s:%d %s", v.Rule, v.File, v.Line, v.Message)
		}
	}
}

func TestLayeringPortCallbacksReport(t *testing.T) {
	root := repoRoot(t)
	result, err := ScanDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, v := range result.Violations {
		if v.Rule == RulePortNoCallback {
			t.Logf("[%s] %s:%d %s", v.Rule, v.File, v.Line, v.Message)
			count++
		}
	}
	t.Logf("LAYER-003: %d callback port violations found", count)
}

func TestLayeringCommandFuncFieldsReport(t *testing.T) {
	root := repoRoot(t)
	result, err := ScanDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, v := range result.Violations {
		if v.Rule == RuleNoHiddenCapability {
			t.Logf("[%s] %s:%d %s", v.Rule, v.File, v.Line, v.Message)
			count++
		}
	}
	t.Logf("LAYER-004: %d command func field violations found", count)
}

func TestLayeringInventoryNonEmpty(t *testing.T) {
	root := repoRoot(t)
	result, err := ScanDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Inventory) == 0 {
		t.Error("inventory is empty; expected model type/func entries")
	}
	t.Logf("inventory: %d items", len(result.Inventory))
}

func TestLayerClassification(t *testing.T) {
	tests := []struct {
		path  string
		layer string
	}{
		{"retrom/internal/model/tagging", "model"},
		{"retrom/internal/service/tagging", "service"},
		{"retrom/internal/repo/tagging", "repo"},
		{"retrom/internal/transport/httpapi", "transport"},
		{"retrom/internal/bootstrap/composition", "bootstrap"},
		{"retrom/internal/adapter/files", "adapter"},
		{"fmt", "external"},
		{"database/sql", "external"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := LayerFromImport(tt.path)
			if got != tt.layer {
				t.Errorf("LayerFromImport(%q) = %q, want %q", tt.path, got, tt.layer)
			}
		})
	}
}

// --- Real fixture-based counter-example tests ---
// These create actual Go source files and run the scanner against them.

func TestCounterExampleRepoImportsService(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "go.mod", "module retrom\ngo 1.23\n")
	writeFixture(t, dir, "internal/repo/bad/bad.go",
		`package bad
import _ "retrom/internal/service/tagging"
`)
	result, err := ScanDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertHasViolation(t, result, RuleModelRepoNoServiceDep)
}

func TestCounterExampleServiceImportsRepo(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "go.mod", "module retrom\ngo 1.23\n")
	writeFixture(t, dir, "internal/service/bad/bad.go",
		`package bad
import _ "retrom/internal/repo/tagging"
`)
	result, err := ScanDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertHasViolation(t, result, RuleServiceNoRepoDep)
}

func TestCounterExampleServiceImportsSQL(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "go.mod", "module retrom\ngo 1.23\n")
	writeFixture(t, dir, "internal/service/bad/bad.go",
		`package bad
import _ "database/sql"
`)
	result, err := ScanDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertHasViolation(t, result, RuleServiceNoRepoDep)
}

func TestCounterExamplePortWithFuncParam(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "go.mod", "module retrom\ngo 1.23\n")
	writeFixture(t, dir, "internal/model/bad/ports.go",
		`package bad
import "context"
type Repository interface {
	CommitWrite(context.Context, func(WriteScope) error) error
}
type WriteScope struct{}
`)
	result, err := ScanDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertHasViolation(t, result, RulePortNoCallback)
}

func TestCounterExampleCommandWithFuncField(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "go.mod", "module retrom\ngo 1.23\n")
	writeFixture(t, dir, "internal/model/bad/types.go",
		`package bad
type MutationCommand struct {
	ID    string
	Apply func(Scope, Snapshot, int64) error
}
type Scope struct{}
type Snapshot struct{}
`)
	result, err := ScanDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertHasViolation(t, result, RuleNoHiddenCapability)
}

func TestCounterExampleServiceReexportTypeAlias(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "go.mod", "module retrom\ngo 1.23\n")
	writeFixture(t, dir, "internal/model/example/types.go",
		`package example
type Item struct{ ID string }
`)
	writeFixture(t, dir, "internal/service/example/types.go",
		`package example
import m "retrom/internal/model/example"
type Item = m.Item
`)
	result, err := ScanDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertHasViolation(t, result, RuleServiceNoModelReexport)
}

func TestCounterExampleServiceReexportVar(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "go.mod", "module retrom\ngo 1.23\n")
	writeFixture(t, dir, "internal/model/example/errors.go",
		`package example
import "errors"
var ErrNotFound = errors.New("not found")
`)
	writeFixture(t, dir, "internal/service/example/types.go",
		`package example
import model "retrom/internal/model/example"
var ErrNotFound = model.ErrNotFound
`)
	result, err := ScanDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertHasViolation(t, result, RuleServiceNoModelReexport)
}

func TestCounterExampleModelCallsTimeNow(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "go.mod", "module retrom\ngo 1.23\n")
	writeFixture(t, dir, "internal/model/bad/impure.go",
		`package bad
import "time"
func Now() int64 { return time.Now().UnixMilli() }
`)
	result, err := ScanDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertHasViolation(t, result, RuleModelNoPureViolation)
}

func TestCounterExampleModelImportsOS(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "go.mod", "module retrom\ngo 1.23\n")
	writeFixture(t, dir, "internal/model/bad/io.go",
		`package bad
import "os"
func Read() { os.Exit(1) }
`)
	result, err := ScanDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertHasViolation(t, result, RuleModelNoPureViolation)
}

func TestCounterExampleNonRepoImportsSQL(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "go.mod", "module retrom\ngo 1.23\n")
	writeFixture(t, dir, "internal/service/bad/db.go",
		`package bad
import _ "database/sql"
`)
	result, err := ScanDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Should be caught by LAYER-002 (service→SQL) or LAYER-008
	hasL002 := hasViolationRule(result, RuleServiceNoRepoDep)
	hasL008 := hasViolationRule(result, RuleNonRepoNoDBCapability)
	if !hasL002 && !hasL008 {
		t.Error("expected LAYER-002 or LAYER-008 violation for service importing SQL")
	}
}

func TestCounterExampleInvalidRootFails(t *testing.T) {
	_, err := ScanDirectory("/nonexistent-path-12345")
	if err == nil {
		t.Error("expected error for nonexistent root, got nil")
	}
}

func TestCounterExampleMissingGoModFails(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal"), 0o755))
	_, err := ScanDirectory(dir)
	if err == nil {
		t.Error("expected error for root without go.mod, got nil")
	}
}

// --- Positive control tests (should NOT be flagged) ---

func TestPositiveControlPureModel(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "go.mod", "module retrom\ngo 1.23\n")
	writeFixture(t, dir, "internal/model/good/types.go",
		`package good
type Tag struct{ ID, Name string }
func Normalize(name string) string { return name }
`)
	result, err := ScanDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Violations) > 0 {
		for _, v := range result.Violations {
			t.Errorf("unexpected violation: [%s] %s:%d %s", v.Rule, v.File, v.Line, v.Message)
		}
	}
}

func TestPositiveControlRepoImportsModel(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "go.mod", "module retrom\ngo 1.23\n")
	writeFixture(t, dir, "internal/repo/good/repo.go",
		`package good
import _ "retrom/internal/model/tagging"
`)
	result, err := ScanDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertNoViolation(t, result, RuleModelRepoNoServiceDep)
}

// --- Helpers ---

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

func assertHasViolation(t *testing.T, result *ScanResult, rule string) {
	t.Helper()
	if !hasViolationRule(result, rule) {
		t.Errorf("expected %s violation but none found; violations: %v", rule, result.Violations)
	}
}

func assertNoViolation(t *testing.T, result *ScanResult, rule string) {
	t.Helper()
	for _, v := range result.Violations {
		if v.Rule == rule {
			t.Errorf("unexpected %s violation: %s:%d %s", rule, v.File, v.Line, v.Message)
		}
	}
}

func hasViolationRule(result *ScanResult, rule string) bool {
	for _, v := range result.Violations {
		if v.Rule == rule {
			return true
		}
	}
	return false
}
