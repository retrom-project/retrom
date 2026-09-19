package pegasusimport

import (
	"math"

	library "retrom/internal/model/libraryimport"
)

// CanCompleteReviewHandoff checks if a review handoff can be completed.
func CanCompleteReviewHandoff(before ReviewHandoffSnapshot, now int64) bool {
	if before.State != "VALIDATING" || before.Version < 1 || before.Version == math.MaxInt64 ||
		before.ImportVersion < 1 || before.ImportVersion == math.MaxInt64 {
		return false
	}
	active := before.ImportState == "RUNNING" && before.JobState == "RUNNING"
	canceling := before.ImportState == "CANCEL_REQUESTED" && before.JobState == "CANCEL_REQUESTED"
	return (active || canceling) && before.Identity.ExecutionNo > 0 && before.Identity.Attempt > 0 &&
		before.Identity.WorkerID != "" &&
		before.LeaseUntilMS > now && before.DeadlineMS > now
}

// MergeReviewMetadataWarnings merges metadata warning additions into existing warnings.
func MergeReviewMetadataWarnings(
	existing []map[string]any,
	additions []library.ServerMetadataWarning,
) []map[string]any {
	result := make([]map[string]any, 0, len(existing)+len(additions))
	result = append(result, existing...)
	for _, addition := range additions {
		duplicate := false
		for _, warning := range result {
			if warning["code"] == addition.Code && warning["field"] == addition.Field {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, map[string]any{"code": addition.Code, "field": addition.Field})
		}
	}
	return result
}
