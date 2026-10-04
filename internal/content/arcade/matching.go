package arcade

import (
	"path/filepath"
	"strings"

	"retrom/internal/importing"
)

func SelectRequirements(facts CatalogRequirements) []ROMRequirement {
	selected := make([]ROMRequirement, 0, len(facts.ROMs))
	for _, rom := range facts.ROMs {
		if rom.Status == "NODUMP" ||
			rom.BIOSName != nil && (facts.DefaultBIOS == nil || *rom.BIOSName != *facts.DefaultBIOS) {
			continue
		}
		selected = append(selected, rom)
	}
	return selected
}

func MatchRequirements(
	entries map[string]importing.ArchiveEntry,
	requirements []ROMRequirement,
) ([]string, []string, []string) {
	foldedEntries := make(map[string]importing.ArchiveEntry, len(entries))
	for name, entry := range entries {
		foldedEntries[importing.ASCIICaseFold(name)] = entry
	}
	var missing, mismatched, warnings []string
	for _, requirement := range requirements {
		entry, exists := foldedEntries[importing.ASCIICaseFold(requirement.Name)]
		if !exists {
			missing = append(missing, requirement.Name)
			continue
		}
		if !entryMatchesRequirement(entry, requirement) {
			mismatched = append(mismatched, requirement.Name)
		}
		if requirement.Status == "BADDUMP" {
			warnings = append(warnings, requirement.Name)
		}
	}
	return missing, mismatched, warnings
}

func entryMatchesRequirement(entry importing.ArchiveEntry, requirement ROMRequirement) bool {
	return entry.Size == requirement.Size &&
		(requirement.CRC32 == nil || strings.EqualFold(entry.CRC32, *requirement.CRC32)) &&
		(requirement.SHA1 == nil || strings.EqualFold(entry.SHA1, *requirement.SHA1))
}

func containsMergedEntries(entries map[string]importing.ArchiveEntry, requirements []ROMRequirement) bool {
	rootEntries := make(map[string]importing.ArchiveEntry, len(entries))
	nestedEntries := make(map[string][]importing.ArchiveEntry)
	for entryPath, entry := range entries {
		if !strings.Contains(entryPath, "/") {
			rootEntries[importing.ASCIICaseFold(entryPath)] = entry
			continue
		}
		base := importing.ASCIICaseFold(filepath.Base(entryPath))
		nestedEntries[base] = append(nestedEntries[base], entry)
	}
	for _, requirement := range requirements {
		name := importing.ASCIICaseFold(requirement.Name)
		if root, exists := rootEntries[name]; exists && entryMatchesRequirement(root, requirement) {
			continue
		}
		for _, nested := range nestedEntries[name] {
			if entryMatchesRequirement(nested, requirement) {
				return true
			}
		}
	}
	return false
}

func requirementNames(requirements []ROMRequirement) []string {
	result := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		result = append(result, requirement.Name)
	}
	return result
}
