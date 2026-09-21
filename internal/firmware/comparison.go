package firmware

import (
	"errors"
	"fmt"
	"strings"

	"retrom/internal/importing"
)

var ErrInvalid = errors.New("BIOS_INSTALLATION_INVALID")

type ArchiveEntryFacts struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
	CRC32     string `json:"crc32,omitempty"`
}

type ArchiveEntryComparison struct {
	Status   string             `json:"status"`
	Expected *ArchiveEntryFacts `json:"expected"`
	Actual   *ArchiveEntryFacts `json:"actual"`
}

func CompareArchiveEntries(
	expected []ExpectedDATEntry,
	actual []importing.ArchiveEntry,
) ([]ArchiveEntryComparison, []map[string]any, []map[string]any, []string) {
	comparisons := make([]ArchiveEntryComparison, 0, len(expected)+len(actual))
	missing := make([]map[string]any, 0)
	mismatched := make([]map[string]any, 0)
	warnings := make([]string, 0)
	used := make(map[int]struct{}, len(actual))
	for _, wanted := range expected {
		exact := findArchiveEntry(actual, used, func(entry importing.ArchiveEntry) bool {
			return strings.EqualFold(entry.NormalizedPath, wanted.Name)
		})
		if exact >= 0 && archiveEntryMatches(wanted, actual[exact]) {
			used[exact] = struct{}{}
			comparisons = append(comparisons, archiveComparison("MATCHED", wanted, &actual[exact]))
			continue
		}
		alias := findArchiveEntry(actual, used, func(entry importing.ArchiveEntry) bool {
			return archiveEntryMatches(wanted, entry)
		})
		if alias >= 0 {
			used[alias] = struct{}{}
			warnings = append(warnings, fmt.Sprintf("%s 已按内容识别为 %s", wanted.Name, actual[alias].NormalizedPath))
			comparisons = append(comparisons, archiveComparison("ALIASED", wanted, &actual[alias]))
			continue
		}
		if exact >= 0 {
			used[exact] = struct{}{}
			mismatched = append(mismatched, archiveEntryDifference(wanted, actual[exact]))
			comparisons = append(comparisons, archiveComparison("MISMATCHED", wanted, &actual[exact]))
			continue
		}
		missing = append(missing, map[string]any{"name": wanted.Name, "expected": expectedEntryFacts(wanted)})
		comparisons = append(comparisons, archiveComparison("MISSING", wanted, nil))
	}
	for index := range actual {
		if _, exists := used[index]; exists {
			continue
		}
		comparisons = append(comparisons, ArchiveEntryComparison{
			Status: "EXTRA",
			Actual: actualEntryFacts(actual[index]),
		})
	}
	return comparisons, missing, mismatched, warnings
}

func archiveComparison(
	status string,
	expected ExpectedDATEntry,
	actual *importing.ArchiveEntry,
) ArchiveEntryComparison {
	comparison := ArchiveEntryComparison{Status: status, Expected: ExpectedDATEntryFacts(expected)}
	if actual != nil {
		comparison.Actual = actualEntryFacts(*actual)
	}
	return comparison
}

func ExpectedDATEntryFacts(entry ExpectedDATEntry) *ArchiveEntryFacts {
	crc32Value := ""
	if entry.CRC32 != "" {
		crc32Value = entry.CRC32
	}
	return &ArchiveEntryFacts{Name: entry.Name, SizeBytes: entry.SizeBytes, CRC32: crc32Value}
}

func actualEntryFacts(entry importing.ArchiveEntry) *ArchiveEntryFacts {
	return &ArchiveEntryFacts{Name: entry.NormalizedPath, SizeBytes: entry.Size, CRC32: entry.CRC32}
}

func findArchiveEntry(
	entries []importing.ArchiveEntry,
	used map[int]struct{},
	matches func(importing.ArchiveEntry) bool,
) int {
	for index, entry := range entries {
		if _, exists := used[index]; !exists && matches(entry) {
			return index
		}
	}
	return -1
}

func archiveEntryMatches(expected ExpectedDATEntry, actual importing.ArchiveEntry) bool {
	if actual.Size != expected.SizeBytes {
		return false
	}
	if expected.SHA1 != "" {
		return strings.EqualFold(actual.SHA1, expected.SHA1)
	}
	return expected.CRC32 != "" && strings.EqualFold(actual.CRC32, expected.CRC32)
}

func expectedEntryFacts(entry ExpectedDATEntry) map[string]any {
	return map[string]any{
		"sizeBytes": entry.SizeBytes,
		"crc32":     nullableString(entry.CRC32),
		"sha1":      nullableString(entry.SHA1),
	}
}

func archiveEntryDifference(expected ExpectedDATEntry, actual importing.ArchiveEntry) map[string]any {
	return map[string]any{
		"name":     expected.Name,
		"expected": expectedEntryFacts(expected),
		"actual": map[string]any{
			"name": actual.NormalizedPath, "sizeBytes": actual.Size, "crc32": actual.CRC32, "sha1": actual.SHA1,
		},
	}
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
