package architecture

import (
	"path"
	"slices"
	"strconv"
	"strings"
)

// OwnershipRegistry is an explicit file list, never a prefix exemption.
type OwnershipRegistry struct {
	SchemaVersion int                `json:"schemaVersion"`
	Baseline      string             `json:"baseline"`
	Packages      []PackageOwnership `json:"packages"`
}

// PackageOwnership assigns exactly one layer, module and refactoring owner.
type PackageOwnership struct {
	Path   string      `json:"path"`
	Layer  string      `json:"layer"`
	Module string      `json:"module"`
	Owner  string      `json:"owner"`
	Files  []OwnedFile `json:"files"`
}

// OwnedFile distinguishes test and tool sources without exempting their imports.
type OwnedFile struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

// Violation identifies a failed rule and the dependency path that caused it.
type Violation struct {
	Rule            string   `json:"rule"`
	File            string   `json:"file"`
	Line            int      `json:"line"`
	Symbol          string   `json:"symbol"`
	DependencyChain []string `json:"dependencyChain"`
	Message         string   `json:"message"`
}

// LoadOwnership rejects unknown fields and multiple JSON documents.
func LoadOwnership(root string) (OwnershipRegistry, error) {
	data, err := readInventoryFile(root, "quality/architecture/package-ownership.json")
	if err != nil {
		return OwnershipRegistry{}, err
	}
	var registry OwnershipRegistry
	if err := decodeStrictJSON(data, &registry); err != nil {
		return OwnershipRegistry{}, err
	}
	return registry, nil
}

// ValidateOwnership compares exact source sets in both directions.
func ValidateOwnership(sources []string, registry OwnershipRegistry) []Violation {
	violations := make([]Violation, 0)
	if registry.SchemaVersion != 1 || !validCommitSHA(registry.Baseline) || len(sources) == 0 {
		return append(violations, ownershipViolation("", "invalid schema, baseline or empty source set"))
	}
	registered := make(map[string]bool)
	packages := make(map[string]bool)
	for _, entry := range registry.Packages {
		violations = append(violations, validateOwnershipEntry(entry, packages, registered)...)
	}
	actual := make(map[string]bool, len(sources))
	for _, name := range sources {
		actual[name] = true
		if !registered[name] {
			violations = append(violations, ownershipViolation(name, "source has no explicit owner"))
		}
	}
	for name := range registered {
		if !actual[name] {
			violations = append(violations, ownershipViolation(name, "registered source is absent"))
		}
	}
	slices.SortFunc(violations, compareViolations)
	return violations
}

func validateOwnershipEntry(entry PackageOwnership, packages, files map[string]bool) []Violation {
	violations := validatePhysicalLayer(entry)
	if !validOwnershipEntry(entry) || packages[entry.Path] {
		violations = append(violations, ownershipViolation(entry.Path, "invalid or duplicate package owner"))
	}
	packages[entry.Path] = true
	for _, file := range entry.Files {
		if !safeRepositoryPath(file.Path) || path.Dir(file.Path) != entry.Path || files[file.Path] {
			violations = append(violations, ownershipViolation(file.Path, "unsafe, duplicate or misplaced source"))
		}
		if !slices.Contains([]string{"production", "test", "tool", "generated"}, file.Kind) {
			violations = append(violations, ownershipViolation(file.Path, "invalid source kind"))
		}
		files[file.Path] = true
	}
	return violations
}

func validOwnershipEntry(entry PackageOwnership) bool {
	layers := []string{
		"cmd", "bootstrap", "model", "capability", "foundation", "service", "repo", "adapter", "transport",
		"testkit", "app", "feature", "component", "lib", "tool", "fixture", "contract", "documentation",
	}
	owner, err := strconv.Atoi(strings.TrimPrefix(entry.Owner, "RF"))
	return (entry.Path == "." || safeRepositoryPath(entry.Path)) &&
		slices.Contains(layers, entry.Layer) && entry.Module != "" &&
		len(entry.Owner) == 4 && strings.HasPrefix(entry.Owner, "RF") &&
		err == nil && owner >= 1 && owner <= 22 && len(entry.Files) > 0
}

func ownershipViolation(file, message string) Violation {
	return Violation{
		Rule: "GOV01", File: file, Line: 1, Symbol: "", DependencyChain: []string{file}, Message: message,
	}
}

func compareViolations(left, right Violation) int {
	if comparison := strings.Compare(left.File, right.File); comparison != 0 {
		return comparison
	}
	if comparison := strings.Compare(left.Rule, right.Rule); comparison != 0 {
		return comparison
	}
	return strings.Compare(left.Message, right.Message)
}
