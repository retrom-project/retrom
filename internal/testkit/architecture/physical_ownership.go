package architecture

import (
	"slices"
	"strings"
)

func validatePhysicalLayer(entry PackageOwnership) []Violation {
	parts := strings.Split(entry.Path, "/")
	if len(parts) < 2 || parts[0] != "internal" {
		return nil
	}
	layers := []string{
		"model", "capability", "foundation", "repo", "service", "adapter", "transport", "bootstrap", "testkit",
	}
	if !slices.Contains(layers, parts[1]) || entry.Layer == parts[1] {
		return nil
	}
	return []Violation{ownershipViolation(entry.Path, "declared layer contradicts the source location")}
}
