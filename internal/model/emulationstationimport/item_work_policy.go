package emulationstationimport

import "math"

// ValidateImportExecution checks that the execution is an active ES import.
func ValidateImportExecution(before LeaseSnapshot, unit Execution, now int64) error {
	if before.Kind != "SERVER_EMULATIONSTATION_IMPORT" || ExecutionState(before, unit, now) != LeaseActive {
		return ErrVersionConflict
	}
	return nil
}

// ValidItemVersion checks that a version is positive and below MaxInt64.
func ValidItemVersion(version int64) bool { return version > 0 && version < math.MaxInt64 }

// WorkingItemState returns true for item states that are part of active processing.
func WorkingItemState(state string) bool {
	return state == "PENDING" || state == "COPYING" || state == "VALIDATING"
}

// ValidItemOutcome validates an item outcome value.
func ValidItemOutcome(outcome ItemOutcome) bool {
	switch outcome.State {
	case "SOURCE_CHANGED", "READ_FAILED", "COMMIT_FAILED":
		return outcome.Code != "" && outcome.ExistingGameID == ""
	case "BLOCKED_CONTENT":
		return outcome.Code != "" && !outcome.Retryable && outcome.ExistingGameID == ""
	case "SKIPPED_EXISTING":
		return outcome.ExistingGameID != "" && len(outcome.ExistingMatches) > 0 && !outcome.Retryable && outcome.Code == ""
	default:
		return false
	}
}
