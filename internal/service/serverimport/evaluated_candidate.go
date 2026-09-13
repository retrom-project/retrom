package serverimport

import (
	"sort"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/files/serversource"
	"retrom/internal/capability/content/firmware"
	"retrom/internal/capability/format/importing"
)

type EvaluatedCandidate struct {
	ID                 string
	Item               CatalogItem
	File               serversource.File
	Association        string
	Metadata           blobstore.Metadata
	ArchiveEntries     []importing.ArchiveEntry
	Static             *firmware.StaticEvaluation
	DAT                *firmware.DATEvaluation
	ExpectedDATEntries []firmware.ExpectedDATEntry
	State              string
	Details            map[string]any
}

func RankCandidates(values []*EvaluatedCandidate) []*EvaluatedCandidate {
	eligible := make([]*EvaluatedCandidate, 0, len(values))
	for _, value := range values {
		if value.State == "ELIGIBLE" {
			eligible = append(eligible, value)
		}
	}
	sort.Slice(eligible, func(left, right int) bool {
		if eligible[left].Static != nil {
			return firmware.CompareStatic(*eligible[left].Static, *eligible[right].Static) < 0
		}
		return firmware.CompareDAT(*eligible[left].DAT, *eligible[right].DAT) < 0
	})
	return eligible
}

func SelectedStatus(candidate *EvaluatedCandidate) (string, string) {
	if candidate.Static != nil {
		return candidate.Static.Status, candidate.Static.Method
	}
	return candidate.DAT.Status, candidate.DAT.Method
}

func StaticStatusMethod(value firmware.StaticEvaluation) (string, string) {
	if value.ExactHash {
		return "MATCHED", "EXACT_HASH"
	}
	if value.ExpectedSizeMatched {
		return "HASH_WARNING", "EXPECTED_SIZE_FALLBACK"
	}
	return "HASH_WARNING", "LARGEST_SIZE_FALLBACK"
}

func DATStatusMethod(value firmware.DATEvaluation) (string, string) {
	if value.MissingCount > 0 {
		return "MISSING_ENTRY", "DAT_PARTIAL_FALLBACK"
	}
	if value.MismatchedCount > 0 {
		return "HASH_WARNING", "DAT_ENTRY_WARNING"
	}
	return "MATCHED", "DAT_ENTRY_MATCH"
}
