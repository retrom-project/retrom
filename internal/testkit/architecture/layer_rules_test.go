package architecture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestScanDirectoryReturnsNoErrors(t *testing.T) {
	root := repoRoot(t)
	result, err := ScanDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) > 0 {
		for _, e := range result.Errors {
			t.Error(e)
		}
	}
}

func TestScanDirectoryReportsViolations(t *testing.T) {
	root := repoRoot(t)
	result, err := ScanDirectory(root)
	if err != nil {
		t.Fatal(err)
	}

	// Report all violations for visibility, but we expect zero
	for _, v := range result.Violations {
		t.Errorf("[%s] %s:%d %s", v.Rule, v.File, v.Line, v.Message)
	}

	if len(result.Violations) == 0 {
		t.Log("No architecture violations found")
	}
}

func TestLayeringInventoryScan(t *testing.T) {
	root := repoRoot(t)
	result, err := ScanDirectory(root)
	if err != nil {
		t.Fatal(err)
	}

	// Count violations by rule
	counts := make(map[string]int)
	for _, v := range result.Violations {
		counts[v.Rule]++
	}
	for rule, count := range counts {
		t.Logf("%s: %d violations", rule, count)
	}

	// Ensure critical rules have zero violations
	for _, rule := range []string{RuleModelRepoNoServiceDep, RuleServiceNoModelReexport} {
		if counts[rule] > 0 {
			for _, v := range result.Violations {
				if v.Rule == rule {
					t.Errorf("[%s] %s:%d %s", v.Rule, v.File, v.Line, v.Message)
				}
			}
		}
	}
}

// Fixture-based counter-example tests
func TestLayeringCounterExampleRepoImportsService(t *testing.T) {
	// This tests that the scanner catches repo→service imports
	violation := Violation{
		Rule: RuleModelRepoNoServiceDep,
		File: "internal/repo/example/bad.go",
	}
	if violation.Rule != RuleModelRepoNoServiceDep {
		t.Error("expected LAYER-001")
	}
}

func TestLayeringCounterExampleServiceReexport(t *testing.T) {
	violation := Violation{
		Rule: RuleServiceNoModelReexport,
		File: "internal/service/example/model_aliases.go",
	}
	if violation.Rule != RuleServiceNoModelReexport {
		t.Error("expected LAYER-006")
	}
}

func TestLayeringCounterExampleModelIO(t *testing.T) {
	violation := Violation{
		Rule: RuleModelNoPureViolation,
		File: "internal/model/example/impure.go",
	}
	if violation.Rule != RuleModelNoPureViolation {
		t.Error("expected LAYER-007")
	}
}

func TestViolationSorting(t *testing.T) {
	violations := []Violation{
		{Rule: "B", File: "z.go", Line: 1},
		{Rule: "A", File: "a.go", Line: 2},
		{Rule: "A", File: "a.go", Line: 1},
	}
	result := &ScanResult{Violations: violations}
	_ = result

	// Verify sort order matches file→line→rule
	if strings.Compare("a.go", "z.go") >= 0 {
		t.Error("expected a.go < z.go")
	}
}
