package pegasusimport

import "math"

// ValidateExecution checks that the given execution identity and timing
// match the current database snapshot, returning ErrVersionConflict if not.
func ValidateExecution(
	before ExecutionSnapshot,
	identity ExecutionIdentity,
	now int64,
) error {
	actual := ExecutionIdentity{
		JobID: before.JobID, ImportID: before.ImportID, WorkerID: before.WorkerID,
		ExecutionNo: before.ExecutionNo, Attempt: before.Attempt,
	}
	if actual != identity || !ValidLiveExecution(before, now) {
		return ErrVersionConflict
	}
	if before.JobState == "CANCEL_REQUESTED" && before.ImportState == "CANCEL_REQUESTED" {
		return nil
	}
	if before.JobState == "RUNNING" {
		if before.Kind == "SERVER_PEGASUS_SCAN" && before.ImportState == "SCANNING" {
			return nil
		}
		if before.Kind == "SERVER_PEGASUS_IMPORT" && before.ImportState == "RUNNING" {
			return nil
		}
	}
	return ErrVersionConflict
}

// ValidLiveExecution checks whether a snapshot represents an active, valid execution.
func ValidLiveExecution(before ExecutionSnapshot, now int64) bool {
	return before.WorkerID != "" && before.ExecutionNo > 0 && before.Attempt > 0 &&
		before.JobVersion > 0 && before.JobVersion < math.MaxInt64 &&
		before.ImportVersion > 0 && before.ImportVersion < math.MaxInt64-1 &&
		before.LeaseUntilMS > now && before.DeadlineMS > now
}
