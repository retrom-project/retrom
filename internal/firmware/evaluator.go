package firmware

import (
	"cmp"
	"slices"
	"strings"

	"retrom/internal/importing"
)

type StaticExpectation struct {
	LogicalName string
	SizeBytes   *int64
	MD5         string
	SHA1        string
	SHA256      string
}

type FileFacts struct {
	RelativePath string
	Basename     string
	SizeBytes    int64
	MD5          string
	SHA1         string
	SHA256       string
	CRC32        string
}

type StaticEvaluation struct {
	Facts               FileFacts
	ExactHash           bool
	ExpectedSizeMatched bool
	ExactBasename       bool
	Status              string
	Method              string
}

func EvaluateStatic(expectation StaticExpectation, facts FileFacts) StaticEvaluation {
	exactHash := expectation.MD5 != "" || expectation.SHA1 != "" || expectation.SHA256 != ""
	exactHash = exactHash &&
		(expectation.SizeBytes == nil || facts.SizeBytes == *expectation.SizeBytes) &&
		(expectation.MD5 == "" || strings.EqualFold(expectation.MD5, facts.MD5)) &&
		(expectation.SHA1 == "" || strings.EqualFold(expectation.SHA1, facts.SHA1)) &&
		(expectation.SHA256 == "" || strings.EqualFold(expectation.SHA256, facts.SHA256))
	expectedSize := expectation.SizeBytes != nil && facts.SizeBytes == *expectation.SizeBytes
	result := StaticEvaluation{
		Facts: facts, ExactHash: exactHash, ExpectedSizeMatched: expectedSize,
		ExactBasename: facts.Basename == expectation.LogicalName, Status: "HASH_WARNING",
		Method: "LARGEST_SIZE_FALLBACK",
	}
	if exactHash {
		result.Status, result.Method = "MATCHED", "EXACT_HASH"
	} else if expectedSize {
		result.Method = "EXPECTED_SIZE_FALLBACK"
	}
	return result
}

func CompareStatic(left, right StaticEvaluation) int {
	if comparison := CompareStaticQuality(left, right); comparison != 0 {
		return comparison
	}
	for _, comparison := range []int{
		cmp.Compare(left.Facts.SHA256, right.Facts.SHA256),
		cmp.Compare(left.Facts.RelativePath, right.Facts.RelativePath),
	} {
		if comparison != 0 {
			return comparison
		}
	}
	return 0
}

// CompareStaticQuality deliberately excludes identity/path tie-breakers. It is
// used for replacement decisions where equal-quality different bytes must not
// displace the current active installation.
func CompareStaticQuality(left, right StaticEvaluation) int {
	for _, comparison := range []int{
		compareTrueFirst(left.ExactHash, right.ExactHash),
		compareTrueFirst(left.ExpectedSizeMatched, right.ExpectedSizeMatched),
		compareTrueFirst(left.ExactBasename, right.ExactBasename),
		cmp.Compare(right.Facts.SizeBytes, left.Facts.SizeBytes),
	} {
		if comparison != 0 {
			return comparison
		}
	}
	return 0
}

type ExpectedDATEntry struct {
	Name      string
	SizeBytes int64
	CRC32     string
	SHA1      string
}

type DATEvaluation struct {
	Facts           FileFacts
	SafeArchive     bool
	Launchable      bool
	MatchedCount    int
	AliasedCount    int
	MismatchedCount int
	MissingCount    int
	ExtraCount      int
	ExactBasename   bool
	Status          string
	Method          string
}

func EvaluateDAT(
	logicalName string,
	expected []ExpectedDATEntry,
	facts FileFacts,
	actual []importing.ArchiveEntry,
) DATEvaluation {
	result := DATEvaluation{Facts: facts, SafeArchive: true, ExactBasename: facts.Basename == logicalName}
	used := make(map[int]struct{}, len(actual))
	for _, wanted := range expected {
		exact := findDATEntry(actual, used, func(entry importing.ArchiveEntry) bool {
			return strings.EqualFold(entry.NormalizedPath, wanted.Name)
		})
		if exact >= 0 && datEntryMatches(wanted, actual[exact]) {
			used[exact] = struct{}{}
			result.MatchedCount++
			continue
		}
		alias := findDATEntry(actual, used, func(entry importing.ArchiveEntry) bool {
			return datEntryMatches(wanted, entry)
		})
		if alias >= 0 {
			used[alias] = struct{}{}
			result.AliasedCount++
			continue
		}
		if exact >= 0 {
			used[exact] = struct{}{}
			result.MismatchedCount++
			continue
		}
		result.MissingCount++
	}
	result.ExtraCount = len(actual) - len(used)
	result.Launchable = len(expected) > 0 && result.MissingCount == 0
	switch {
	case result.MissingCount > 0:
		result.Status, result.Method = "MISSING_ENTRY", "DAT_PARTIAL_FALLBACK"
	case result.MismatchedCount > 0:
		result.Status, result.Method = "HASH_WARNING", "DAT_ENTRY_WARNING"
	default:
		result.Status, result.Method = "MATCHED", "DAT_ENTRY_MATCH"
	}
	return result
}

func CompareDAT(left, right DATEvaluation) int {
	if comparison := CompareDATQuality(left, right); comparison != 0 {
		return comparison
	}
	for _, comparison := range []int{
		cmp.Compare(left.Facts.SHA256, right.Facts.SHA256),
		cmp.Compare(left.Facts.RelativePath, right.Facts.RelativePath),
	} {
		if comparison != 0 {
			return comparison
		}
	}
	return 0
}

// CompareDATQuality excludes deterministic candidate identity/path
// tie-breakers for active-installation replacement decisions.
func CompareDATQuality(left, right DATEvaluation) int {
	for _, comparison := range []int{
		compareTrueFirst(left.SafeArchive, right.SafeArchive),
		compareTrueFirst(left.Launchable, right.Launchable),
		cmp.Compare(right.MatchedCount+right.AliasedCount, left.MatchedCount+left.AliasedCount),
		cmp.Compare(right.MatchedCount, left.MatchedCount),
		cmp.Compare(left.MismatchedCount, right.MismatchedCount),
		cmp.Compare(left.ExtraCount, right.ExtraCount),
		compareTrueFirst(left.ExactBasename, right.ExactBasename),
		cmp.Compare(right.Facts.SizeBytes, left.Facts.SizeBytes),
	} {
		if comparison != 0 {
			return comparison
		}
	}
	return 0
}

func SortStatic(values []StaticEvaluation) { slices.SortFunc(values, CompareStatic) }
func SortDAT(values []DATEvaluation)       { slices.SortFunc(values, CompareDAT) }

func compareTrueFirst(left, right bool) int {
	if left == right {
		return 0
	}
	if left {
		return -1
	}
	return 1
}

func findDATEntry(
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

func datEntryMatches(expected ExpectedDATEntry, actual importing.ArchiveEntry) bool {
	if expected.SizeBytes != actual.Size {
		return false
	}
	if expected.SHA1 != "" {
		return strings.EqualFold(expected.SHA1, actual.SHA1)
	}
	return expected.CRC32 != "" && strings.EqualFold(expected.CRC32, actual.CRC32)
}
