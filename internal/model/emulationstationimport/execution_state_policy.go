package emulationstationimport

import "math"

// ExecutionState validates the current transaction snapshot against the claimed attempt.
func ExecutionState(before LeaseSnapshot, unit Execution, now int64) LeaseState {
	if before.Execution != unit || unit.WorkerID == "" || unit.ExecutionNo <= 0 || unit.Attempt <= 0 ||
		before.JobVersion <= 0 || before.JobVersion == math.MaxInt64 || before.ImportVersion <= 0 ||
		before.ImportVersion == math.MaxInt64 {
		return LeaseLost
	}
	if before.JobState == "CANCEL_REQUESTED" && before.ImportState == "CANCEL_REQUESTED" {
		return LeaseCancelled
	}
	if before.JobState != "RUNNING" || !runningImportState(before) {
		return LeaseLost
	}
	if before.DeadlineAtMS <= now {
		return LeaseDeadline
	}
	if before.LeaseUntilMS <= now {
		return LeaseLost
	}
	return LeaseActive
}

func runningImportState(before LeaseSnapshot) bool {
	return (before.Kind == "SERVER_EMULATIONSTATION_SCAN" && before.ImportState == "SCANNING") ||
		(before.Kind == "SERVER_EMULATIONSTATION_IMPORT" && before.ImportState == "RUNNING")
}
