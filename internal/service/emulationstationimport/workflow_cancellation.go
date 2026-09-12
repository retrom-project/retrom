package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type JobCancellationRequest struct {
	JobID, Kind, ScopeID, Reason, ActorID string
	ExpectedVersion                       int64
}
type JobCancellationResult struct {
	JobID, State         string
	ExecutionNo, Version int64
}
type cancellationRequest struct {
	ID, Reason, ActorID string
	Version             int64
	Job                 *JobCancellationRequest
}

func (service *WorkflowControl) Cancel(
	ctx context.Context,
	id string,
	version int64,
	reason, actor string,
) (Summary, bool, error) {
	result, pending, err := service.cancel(ctx, cancellationRequest{
		ID: id, Version: version, Reason: reason, ActorID: actor,
	})
	return result.Summary, pending, err
}

// CancelJob validates the original Job ETag in the transaction that cancels its linked plan.
func (service *WorkflowControl) CancelJob(
	ctx context.Context,
	request JobCancellationRequest,
) (JobCancellationResult, bool, error) {
	after, pending, err := service.cancel(ctx, cancellationRequest{
		ID: request.ScopeID, Version: request.ExpectedVersion, Reason: request.Reason,
		ActorID: request.ActorID, Job: &request,
	})
	if err != nil {
		return JobCancellationResult{}, false, err
	}
	return JobCancellationResult{
		JobID: request.JobID, State: after.JobState, Version: after.JobVersion, ExecutionNo: after.Execution,
	}, pending, nil
}

func (service *WorkflowControl) cancel(
	ctx context.Context,
	request cancellationRequest,
) (WorkflowSnapshot, bool, error) {
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || len([]rune(request.Reason)) > 500 {
		return WorkflowSnapshot{}, false, ErrNotCancellable
	}
	var result WorkflowSnapshot
	var pending bool
	err := service.repository.WithControl(ctx, func(scope WorkflowScope) error {
		before, err := scope.Read.Current(ctx, request.ID)
		if errors.Is(err, ErrNotFound) {
			return ErrNotCancellable
		}
		if err != nil {
			return fmt.Errorf("read EmulationStation cancellation: %w", err)
		}
		if err := validateCancellation(before, request); err != nil {
			return err
		}
		plan, err := service.cancellationPlan(before, request)
		if err != nil {
			return err
		}
		if err := scope.Write.Cancel(ctx, plan); err != nil {
			return fmt.Errorf("persist EmulationStation cancellation: %w", err)
		}
		after, err := scope.Read.Current(ctx, before.Summary.ID)
		if err != nil {
			return fmt.Errorf("read cancelled EmulationStation plan: %w", err)
		}
		result, pending = after, plan.Pending
		return nil
	})
	if err != nil {
		return WorkflowSnapshot{}, false, fmt.Errorf("finish EmulationStation cancellation: %w", err)
	}
	return result, pending, nil
}

func validateCancellation(before WorkflowSnapshot, request cancellationRequest) error {
	version := request.Version
	if request.Job != nil {
		if !matchesCancellationJob(before, *request.Job) {
			return ErrNotCancellable
		}
		if version < 1 || version != before.JobVersion {
			return ErrVersionConflict
		}
		version = before.Summary.Version
	}
	if !canCancel(before, version) {
		return ErrNotCancellable
	}
	return nil
}

func matchesCancellationJob(before WorkflowSnapshot, request JobCancellationRequest) bool {
	if request.ScopeID != before.Summary.ID {
		return false
	}
	if before.Summary.ImportJobID != nil {
		return request.JobID == *before.Summary.ImportJobID && request.Kind == "SERVER_EMULATIONSTATION_IMPORT"
	}
	return request.JobID == before.Summary.ScanJobID && request.Kind == "SERVER_EMULATIONSTATION_SCAN"
}

func (service *WorkflowControl) cancellationPlan(
	before WorkflowSnapshot,
	request cancellationRequest,
) (CancellationPlan, error) {
	audit, err := uuid.NewV7()
	if err != nil {
		return CancellationPlan{}, fmt.Errorf("generate EmulationStation cancellation audit: %w", err)
	}
	plan := CancellationPlan{
		Before: before, Reason: request.Reason, ActorID: request.ActorID, AuditID: audit.String(),
		NowMS: service.now().UnixMilli(), State: "CANCELLED", Pending: before.JobState == "RUNNING",
	}
	if plan.Pending {
		plan.State = "CANCEL_REQUESTED"
	} else {
		plan.CompletedAtMS = &plan.NowMS
	}
	return plan, nil
}
