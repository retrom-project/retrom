package emulationstationimport

import (
	"math"
	model "retrom/internal/model/emulationstationimport"
)

func validateImportExecution(before model.LeaseSnapshot, unit model.Execution, now int64) error {
	if before.Kind != "SERVER_EMULATIONSTATION_IMPORT" || ExecutionState(before, unit, now) != model.LeaseActive {
		return model.ErrVersionConflict
	}
	return nil
}
func validItemVersion(version int64) bool { return version > 0 && version < math.MaxInt64 }
func workingItemState(state string) bool {
	return state == "PENDING" || state == "COPYING" || state == "VALIDATING"
}

func validItemOutcome(outcome model.ItemOutcome) bool {
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
