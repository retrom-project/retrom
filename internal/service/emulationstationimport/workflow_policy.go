package emulationstationimport

import (
	"fmt"
	"math"
	model "retrom/internal/model/emulationstationimport"

	"github.com/google/uuid"
)

func validWorkflowVersion(before model.WorkflowSnapshot, version int64) bool {
	return version > 0 && version < math.MaxInt64 && before.Summary.Version == version &&
		before.JobVersion > 0 && before.JobVersion < math.MaxInt64 &&
		before.Execution > 0 && (before.Summary.ImportJobID != nil || before.Summary.ScanJobID != "")
}

func canCancel(before model.WorkflowSnapshot, version int64) bool {
	if !validWorkflowVersion(before, version) {
		return false
	}
	if before.Summary.ImportJobID == nil {
		return before.Summary.State == "SCANNING" && (before.JobState == "QUEUED" || before.JobState == "RUNNING")
	}
	return (before.Summary.State == "QUEUED" && before.JobState == "QUEUED") ||
		(before.Summary.State == "RUNNING" && before.JobState == "RUNNING")
}

func validateRetry(before model.RetrySnapshot, version int64) error {
	if before.Summary.ImportJobID == nil || !validWorkflowVersion(before.WorkflowSnapshot, version) ||
		before.Execution == math.MaxInt64 ||
		!before.Summary.Retryable || before.RetryableItems == 0 {
		return model.ErrNotRetryable
	}
	if (before.Summary.State != "FAILED" && before.Summary.State != "PARTIAL_FAILURE") ||
		(before.JobState != "FAILED" && before.JobState != "SUCCEEDED") {
		return model.ErrNotRetryable
	}
	if before.OtherActive {
		return model.ErrActive
	}
	if !before.TargetsValid {
		return model.ErrMappingTargetChanged
	}
	return nil
}

func sameRetryExecution(before, current model.RetrySnapshot) bool {
	return *before.Summary.ImportJobID == *current.Summary.ImportJobID && before.JobVersion == current.JobVersion &&
		before.Execution == current.Execution && before.Summary.MappingVersion == current.Summary.MappingVersion
}

func newRetryPlan(before model.RetrySnapshot, actor string) (model.RetryPlan, error) {
	plan := model.RetryPlan{Before: before, Execution: before.Execution + 1, ActorID: actor}
	for _, target := range []*string{&plan.ExecutionID, &plan.AuditID} {
		id, err := uuid.NewV7()
		if err != nil {
			return model.RetryPlan{}, fmt.Errorf("generate EmulationStation retry identity: %w", err)
		}
		*target = id.String()
	}
	return plan, nil
}
