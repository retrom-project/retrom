package pegasusimport

import "math"

func ValidWorkflowVersion(before WorkflowSnapshot, version int64) bool {
	return version > 0 && version < math.MaxInt64 && before.Summary.Version == version &&
		before.JobVersion > 0 && before.JobVersion < math.MaxInt64 && before.Execution > 0
}

func CanCancelWorkflow(before WorkflowSnapshot, version int64) bool {
	if !ValidWorkflowVersion(before, version) || (before.JobState != "QUEUED" && before.JobState != "RUNNING") {
		return false
	}
	if before.Summary.ImportJobID == nil {
		return before.Summary.ScanJobID != "" && before.Summary.State == "SCANNING"
	}
	return before.Summary.State == "QUEUED" || before.Summary.State == "RUNNING"
}

func CanRetry(before WorkflowSnapshot, version int64) bool {
	if !ValidWorkflowVersion(before, version) || before.Execution == math.MaxInt64 ||
		before.Summary.ImportJobID == nil {
		return false
	}
	if !before.Summary.Retryable || before.OtherActive || before.RetryableItems == 0 {
		return false
	}
	return (before.Summary.State == "FAILED" || before.Summary.State == "PARTIAL_FAILURE") &&
		(before.JobState == "FAILED" || before.JobState == "SUCCEEDED")
}

func MatchesCancellationJob(before WorkflowSnapshot, jobID, kind, scopeID string) bool {
	if before.Summary.ID != scopeID {
		return false
	}
	if before.Summary.ImportJobID != nil {
		return *before.Summary.ImportJobID == jobID && kind == "SERVER_PEGASUS_IMPORT"
	}
	return before.Summary.ScanJobID == jobID && kind == "SERVER_PEGASUS_SCAN"
}
