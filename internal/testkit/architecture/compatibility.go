package architecture

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"
)

// CompatibilityReport describes changes to byte-stable public inputs.
type CompatibilityReport struct {
	Baseline   string      `json:"baseline"`
	Files      []string    `json:"files"`
	Violations []Violation `json:"violations"`
}

// InspectCompatibility compares Git's baseline tree with tracked and untracked inputs.
func InspectCompatibility(ctx context.Context, root, baseline string) (CompatibilityReport, error) {
	if !validCommitSHA(baseline) {
		return CompatibilityReport{}, ErrInvalidBaseline
	}
	paths := []string{"api", "migrations", "data", "workspace/manifest.yaml", "testdata/public-roms"}
	arguments := append([]string{"ls-tree", "-r", "--name-only", "-z", baseline, "--"}, paths...)
	original, err := inventoryGit(ctx, root, arguments...)
	if err != nil {
		return CompatibilityReport{}, err
	}
	arguments = append([]string{"ls-files", "--cached", "--others", "--exclude-standard", "-z", "--"}, paths...)
	current, err := inventoryGit(ctx, root, arguments...)
	if err != nil {
		return CompatibilityReport{}, err
	}
	arguments = append([]string{"diff", "--name-only", "-z", baseline, "--"}, paths...)
	changed, err := inventoryGit(ctx, root, arguments...)
	if err != nil {
		return CompatibilityReport{}, err
	}
	report := CompatibilityReport{Baseline: baseline, Files: compatibilityPaths(original), Violations: []Violation{}}
	if len(report.Files) == 0 {
		return CompatibilityReport{}, fmt.Errorf("%w: empty compatibility input set", ErrEmptySources)
	}
	for _, name := range compatibilityPaths(changed) {
		report.Violations = append(report.Violations, compatibilityViolation(name, "frozen input differs from baseline"))
	}
	for _, name := range compatibilityPaths(current) {
		if !slices.Contains(report.Files, name) {
			report.Violations = append(report.Violations, compatibilityViolation(name, "unapproved compatibility input"))
		}
	}
	slices.SortFunc(report.Violations, compareViolations)
	return report, nil
}

func compatibilityPaths(output string) []string {
	result := make([]string, 0)
	for _, name := range strings.Split(output, "\x00") {
		if compatibilityPath(name) {
			result = append(result, name)
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}

func compatibilityPath(name string) bool {
	switch {
	case strings.HasPrefix(name, "api/"):
		return slices.Contains([]string{".json", ".yaml", ".yml"}, path.Ext(name))
	case strings.HasPrefix(name, "migrations/"):
		return path.Ext(name) == ".sql"
	case strings.HasPrefix(name, "data/"), strings.HasPrefix(name, "testdata/public-roms/"):
		return true
	default:
		return name == "workspace/manifest.yaml"
	}
}

func compatibilityViolation(file, message string) Violation {
	return Violation{
		Rule: "AR11", File: file, Line: 1, DependencyChain: []string{file}, Message: message,
	}
}
