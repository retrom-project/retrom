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

type StaticChecks struct {
	Size   string `json:"size"`
	MD5    string `json:"md5"`
	SHA1   string `json:"sha1"`
	SHA256 string `json:"sha256"`
}

type StaticEvaluation struct {
	Checks              StaticChecks
	Facts               FileFacts
	ExactHash           bool
	ExpectedSizeMatched bool
	ExactBasename       bool
	Status              string
	Method              string
}

func EvaluateStatic(expectation StaticExpectation, facts FileFacts) StaticEvaluation {
	checks := StaticChecks{
		Size: "NOT_CHECKED", MD5: hashCheck(expectation.MD5, facts.MD5),
		SHA1: hashCheck(expectation.SHA1, facts.SHA1), SHA256: hashCheck(expectation.SHA256, facts.SHA256),
	}
	if expectation.SizeBytes != nil {
		checks.Size = "MISMATCHED"
		if facts.SizeBytes == *expectation.SizeBytes {
			checks.Size = "MATCHED"
		}
	}
	declaredHash := checks.MD5 != "NOT_CHECKED" || checks.SHA1 != "NOT_CHECKED" || checks.SHA256 != "NOT_CHECKED"
	mismatch := checks.Size == "MISMATCHED" || checks.MD5 == "MISMATCHED" ||
		checks.SHA1 == "MISMATCHED" || checks.SHA256 == "MISMATCHED"
	result := StaticEvaluation{
		Facts: facts, Checks: checks, ExactHash: declaredHash && !mismatch,
		ExpectedSizeMatched: checks.Size == "MATCHED", ExactBasename: facts.Basename == expectation.LogicalName,
		Status: "UNVERIFIED", Method: "LARGEST_SIZE_FALLBACK",
	}
	if mismatch {
		result.Status = "HASH_WARNING"
	}
	if result.ExactHash {
		result.Status, result.Method = "MATCHED", "EXACT_HASH"
	} else if result.ExpectedSizeMatched {
		result.Method = "EXPECTED_SIZE_FALLBACK"
	}
	return result
}

func hashCheck(expected, actual string) string {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return "NOT_CHECKED"
	}
	if strings.EqualFold(expected, actual) {
		return "MATCHED"
	}
	return "MISMATCHED"
}

// OptionalHash is the canonical catalog boundary for absent hash requirements.
func OptionalHash(value string) *string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return nil
	}
	return &value
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
	comparisons, _, _, _ := CompareArchiveEntries(expected, actual)
	for _, comparison := range comparisons {
		switch comparison.Status {
		case "MATCHED":
			result.MatchedCount++
		case "ALIASED":
			result.AliasedCount++
		case "MISMATCHED":
			result.MismatchedCount++
		case "MISSING":
			result.MissingCount++
		case "EXTRA":
			result.ExtraCount++
		}
	}
	result.Launchable = true
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
		compareTrueFirst(left.MissingCount == 0, right.MissingCount == 0),
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
