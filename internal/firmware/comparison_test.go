package firmware

import (
	"testing"

	"retrom/internal/importing"
)

func TestDATDuplicateDeclarationsDescribeOneRequiredMember(t *testing.T) {
	t.Parallel()
	wanted := ExpectedDATEntry{Name: "sm1.sm1", SizeBytes: 4, CRC32: "abcd", SHA1: "digest"}
	actual := []importing.ArchiveEntry{{NormalizedPath: "sm1.sm1", Size: 4, CRC32: "abcd", SHA1: "digest"}}
	expected := []ExpectedDATEntry{wanted, wanted}
	comparisons, missing, mismatched, warnings := CompareArchiveEntries(expected, actual)
	if len(comparisons) != 1 || len(missing)+len(mismatched)+len(warnings) != 0 {
		t.Fatalf("duplicate became another requirement: %+v missing=%v mismatched=%v", comparisons, missing, mismatched)
	}
	result := EvaluateDAT("bios.zip", expected, FileFacts{Basename: "bios.zip"}, actual)
	if result.Status != "MATCHED" || result.MatchedCount != 1 || result.MissingCount != 0 {
		t.Fatalf("scan disagrees with inspection: %+v", result)
	}
}

func TestDATDistinctNamesAndConflictingRequirementsRemainRequired(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"name", "size", "crc", "sha1"} {
		t.Run(change, func(t *testing.T) {
			first := ExpectedDATEntry{Name: "first.rom", SizeBytes: 4, CRC32: "abcd", SHA1: "digest"}
			second := first
			switch change {
			case "name":
				second.Name = "second.rom"
			case "size":
				second.SizeBytes++
			case "crc":
				second.CRC32 = "other"
			case "sha1":
				second.SHA1 = "other"
			}
			actual := []importing.ArchiveEntry{{NormalizedPath: "first.rom", Size: 4, CRC32: "abcd", SHA1: "digest"}}
			comparisons, missing, _, _ := CompareArchiveEntries([]ExpectedDATEntry{first, second}, actual)
			if len(comparisons) != 2 || len(missing) != 1 {
				t.Fatalf("distinct %s requirement discarded: %+v", change, comparisons)
			}
		})
	}
}
