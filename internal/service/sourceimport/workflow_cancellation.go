package sourceimport

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

type JobCancellationRequest struct {
	JobID, Kind, ScopeID, Reason, ActorID string
}
type workflowCancellationRequest struct {
	JobCancellationRequest
	ImportID      string
	ImportVersion int64
}

// CancelForDiscard retains the aggregate guard for the distinct destructive discard operation.
func (service *WorkflowControl) CancelForDiscard(ctx context.Context, id string, version int64, reason, actorID string) (Summary, bool, error) {
	after, pending, err := service.cancel(ctx, workflowCancellationRequest{
		JobCancellationRequest: JobCancellationRequest{Reason: reason, ActorID: actorID},
		ImportID:               id, ImportVersion: version,
	})
	return after.Summary, pending, err
}

type JobCancellationResult struct {
	JobID, State         string
	ExecutionNo, Version int64
}

// CancelJob reads the latest execution in the same transaction as domain cancellation.
func (service *WorkflowControl) CancelJob(
	ctx context.Context,
	request JobCancellationRequest,
) (JobCancellationResult, bool, error) {
	after, pending, err := service.cancel(ctx, workflowCancellationRequest{JobCancellationRequest: request})
	if err != nil {
		return JobCancellationResult{}, false, err
	}
	return JobCancellationResult{
		JobID:       request.JobID,
		State:       after.JobState,
		Version:     after.JobVersion,
		ExecutionNo: after.Execution,
	}, pending, nil
}

func (service *WorkflowControl) cancel(
	ctx context.Context,
	request workflowCancellationRequest,
) (WorkflowSnapshot, bool, error) {
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || utf8.RuneCountInString(request.Reason) > 500 {
		return WorkflowSnapshot{}, false, ErrNotCancellable
	}
	var result WorkflowSnapshot
	var pending bool
	err := service.repository.WithControl(ctx, func(scope WorkflowScope) error {
		before, err := readCancellation(ctx, scope.Read, request)
		if err != nil {
			return err
		}
		if before.JobState == "CANCEL_REQUESTED" || before.JobState == "CANCELLED" {
			result, pending = before, before.JobState == "CANCEL_REQUESTED"
			return nil
		}
		plan, err := service.cancellationPlan(before, request.JobCancellationRequest)
		if err != nil {
			return err
		}
		if err := scope.Write.Cancel(ctx, plan); err != nil {
			return fmt.Errorf("save Source cancellation: %w", err)
		}
		if plan.Before.Summary.ImportJobID != nil {
			if err := scheduleTerminalPayloads(ctx, scope.Payload, plan.Before.Summary.ID, plan.NowMS); err != nil {
				return err
			}
		}
		after, err := scope.Read.Current(ctx, before.Summary.ID)
		if err != nil {
			return fmt.Errorf("read cancelled Source import: %w", err)
		}
		result, pending = after, plan.Pending
		return nil
	})
	if err != nil {
		return WorkflowSnapshot{}, false, fmt.Errorf("finish Source cancellation: %w", err)
	}
	return result, pending, nil
}

func readCancellation(
	ctx context.Context,
	reader WorkflowReader,
	request workflowCancellationRequest,
) (WorkflowSnapshot, error) {
	if request.ImportID != "" {
		before, err := reader.Current(ctx, request.ImportID)
		if err != nil {
			return WorkflowSnapshot{}, fmt.Errorf("read discard cancellation: %w", err)
		}
		if !validWorkflowVersion(before, request.ImportVersion) || !canCancel(before) {
			return WorkflowSnapshot{}, ErrNotCancellable
		}
		return before, nil
	}
	before, err := reader.CurrentJob(ctx, request.JobID)
	if err != nil {
		return WorkflowSnapshot{}, fmt.Errorf("read Source cancellation: %w", err)
	}
	if !matchesCancellationJob(before, request.JobCancellationRequest) {
		return WorkflowSnapshot{}, ErrNotCancellable
	}
	if before.JobState != "CANCEL_REQUESTED" && before.JobState != "CANCELLED" && !canCancel(before) {
		return WorkflowSnapshot{}, ErrNotCancellable
	}
	return before, nil
}

func (service *WorkflowControl) cancellationPlan(
	before WorkflowSnapshot,
	request JobCancellationRequest,
) (CancellationPlan, error) {
	auditID, err := uuid.NewV7()
	if err != nil {
		return CancellationPlan{}, fmt.Errorf("generate Source cancellation audit: %w", err)
	}
	plan := CancellationPlan{
		Before: before, Reason: request.Reason, ActorID: request.ActorID, AuditID: auditID.String(),
		NowMS: service.now().UnixMilli(), State: "CANCELLED",
		Pending: before.JobState == "RUNNING" || before.Summary.State == "RUNNING",
	}
	if plan.Pending {
		plan.State = "CANCEL_REQUESTED"
	} else {
		plan.CompletedAtMS = &plan.NowMS
	}
	return plan, nil
}

func canCancel(before WorkflowSnapshot) bool {
	if before.JobState != "QUEUED" && before.JobState != "RUNNING" {
		return false
	}
	if before.Summary.ImportJobID == nil {
		return before.Summary.ScanJobID != "" && before.Summary.State == "SCANNING"
	}
	return before.Summary.State == "QUEUED" || before.Summary.State == "RUNNING"
}

func matchesCancellationJob(before WorkflowSnapshot, request JobCancellationRequest) bool {
	if before.Summary.ID != request.ScopeID {
		return false
	}
	if before.Summary.ImportJobID != nil {
		return *before.Summary.ImportJobID == request.JobID && request.Kind == "IMPORT_RECEIVE"
	}
	return before.Summary.ScanJobID == request.JobID && request.Kind == "IMPORT_SCAN"
}
