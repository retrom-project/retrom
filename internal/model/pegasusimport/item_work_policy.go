package pegasusimport

import "math"

// ValidItemVersion checks that a version is positive and below MaxInt64.
func ValidItemVersion(version int64) bool { return version > 0 && version < math.MaxInt64 }

// WorkingItemState returns true for item states that are part of active processing.
func WorkingItemState(state string) bool {
	return state == "PENDING" || state == "COPYING" || state == "VALIDATING"
}

// ValidItemOutcome validates an item outcome value.
func ValidItemOutcome(outcome ItemOutcome) bool {
	switch outcome.State {
	case "SOURCE_CHANGED", "READ_FAILED", "COMMIT_FAILED", "BLOCKED_CONTENT", "CANCELLED":
		return outcome.Code != ""
	case "SKIPPED_EXISTING":
		return outcome.ExistingGameID != "" && len(outcome.ExistingMatches) > 0
	default:
		return false
	}
}
