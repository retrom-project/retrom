package emulationstationimport

import "math"

func ValidWorkflowVersion(before WorkflowSnapshot, version int64) bool {
	return version > 0 && version < math.MaxInt64 && before.Summary.Version == version &&
		before.JobVersion > 0 && before.JobVersion < math.MaxInt64 &&
		before.Execution > 0 && (before.Summary.ImportJobID != nil || before.Summary.ScanJobID != "")
}

func CanCancelWorkflow(before WorkflowSnapshot, version int64) bool {
	if !ValidWorkflowVersion(before, version) {
		return false
	}
	if before.Summary.ImportJobID == nil {
		return before.Summary.State == "SCANNING" && (before.JobState == "QUEUED" || before.JobState == "RUNNING")
	}
	return (before.Summary.State == "QUEUED" && before.JobState == "QUEUED") ||
		(before.Summary.State == "RUNNING" && before.JobState == "RUNNING")
}

func MatchesCancellationJob(before WorkflowSnapshot, jobID, kind, scopeID string) bool {
	if scopeID != before.Summary.ID {
		return false
	}
	if before.Summary.ImportJobID != nil {
		return jobID == *before.Summary.ImportJobID && kind == "SERVER_EMULATIONSTATION_IMPORT"
	}
	return before.Summary.ScanJobID == jobID && kind == "SERVER_EMULATIONSTATION_SCAN"
}

func ValidateRetryEligibility(before RetrySnapshot, version int64) error {
	if before.Summary.ImportJobID == nil || !ValidWorkflowVersion(before.WorkflowSnapshot, version) ||
		before.Execution == math.MaxInt64 ||
		!before.Summary.Retryable || before.RetryableItems == 0 {
		return ErrNotRetryable
	}
	if (before.Summary.State != "FAILED" && before.Summary.State != "PARTIAL_FAILURE") ||
		(before.JobState != "FAILED" && before.JobState != "SUCCEEDED") {
		return ErrNotRetryable
	}
	if before.OtherActive {
		return ErrActive
	}
	if !before.TargetsValid {
		return ErrMappingTargetChanged
	}
	return nil
}

func SameRetryExecution(before, current RetrySnapshot) bool {
	return *before.Summary.ImportJobID == *current.Summary.ImportJobID && before.JobVersion == current.JobVersion &&
		before.Execution == current.Execution && before.Summary.MappingVersion == current.Summary.MappingVersion
}

func SameFrozenSource(
	beforeSummary, currentSummary Summary,
	before, current FrozenSourceSnapshot,
) bool {
	if before.RootConfigDigest != current.RootConfigDigest ||
		before.SourceSnapshotDigest != current.SourceSnapshotDigest ||
		before.ReleaseYearMax != current.ReleaseYearMax || beforeSummary.Root.ID != currentSummary.Root.ID ||
		beforeSummary.SourceRelativePath != currentSummary.SourceRelativePath ||
		len(before.Gamelists) != len(current.Gamelists) {
		return false
	}
	for i, left := range before.Gamelists {
		right := current.Gamelists[i]
		if left.RelativePath != right.RelativePath || left.FactsDigest != right.FactsDigest ||
			left.ParseState != right.ParseState || left.SizeBytes != right.SizeBytes {
			return false
		}
		if (left.ContentDigest == nil) != (right.ContentDigest == nil) {
			return false
		}
		if left.ContentDigest != nil && *left.ContentDigest != *right.ContentDigest {
			return false
		}
	}
	return true
}
