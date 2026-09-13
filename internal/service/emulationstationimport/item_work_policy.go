package emulationstationimport

import "math"

func validateImportExecution(before LeaseSnapshot, unit Execution, now int64) error {
	if before.Kind != "SERVER_EMULATIONSTATION_IMPORT" || ExecutionState(before, unit, now) != LeaseActive {
		return ErrVersionConflict
	}
	return nil
}
func validItemVersion(version int64) bool { return version > 0 && version < math.MaxInt64 }
func workingItemState(state string) bool {
	return state == "PENDING" || state == "COPYING" || state == "VALIDATING"
}

func validItemOutcome(outcome ItemOutcome) bool {
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
