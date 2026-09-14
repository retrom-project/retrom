package pegasusimport

type ExecutionSnapshot struct {
	JobID, ImportID, Kind, JobState, ImportState, WorkerID       string
	JobVersion, ImportVersion, ExecutionNo, Attempt, MaxAttempts int64
	LeaseUntilMS, DeadlineMS                                     int64
}
