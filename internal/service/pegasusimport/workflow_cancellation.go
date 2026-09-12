package pegasusimport

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

type cancellationRequest struct {
	ID, Reason, ActorID, ScopeID, Kind string
	Version                            int64
	ByJob                              bool
}
type JobCancellationRequest struct {
	JobID, Kind, ScopeID, Reason, ActorID string
	ExpectedVersion                       int64
}
type JobCancellationResult struct {
	JobID, State         string
	ExecutionNo, Version int64
}

func (service *WorkflowControl) Cancel(
	ctx context.Context,
	id string,
	version int64,
	reason, actorID string,
) (Summary, bool, error) {
	after, pending, err := service.cancel(
		ctx,
		cancellationRequest{ID: id, Version: version, Reason: reason, ActorID: actorID},
	)
	return after.Summary, pending, err
}

// CancelJob checks the caller's original Job version within the same transaction
// that changes the domain aggregate; it never translates an ETag outside that transaction.
func (service *WorkflowControl) CancelJob(
	ctx context.Context,
	request JobCancellationRequest,
) (JobCancellationResult, bool, error) {
	after, pending, err := service.cancel(ctx, cancellationRequest{
		ID: request.JobID, Version: request.ExpectedVersion,
		Reason: request.Reason, ActorID: request.ActorID, ByJob: true, ScopeID: request.ScopeID, Kind: request.Kind,
	})
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
	request cancellationRequest,
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
		plan, err := service.cancellationPlan(before, request)
		if err != nil {
			return err
		}
		if err := scope.Write.Cancel(ctx, plan); err != nil {
			return fmt.Errorf("save Pegasus cancellation: %w", err)
		}
		after, err := scope.Read.Current(ctx, before.Summary.ID)
		if err != nil {
			return fmt.Errorf("read cancelled Pegasus import: %w", err)
		}
		result, pending = after, plan.Pending
		return nil
	})
	if err != nil {
		return WorkflowSnapshot{}, false, fmt.Errorf("finish Pegasus cancellation: %w", err)
	}
	return result, pending, nil
}

func readCancellation(
	ctx context.Context,
	reader WorkflowReader,
	request cancellationRequest,
) (WorkflowSnapshot, error) {
	var before WorkflowSnapshot
	var err error
	if request.ByJob {
		before, err = reader.CurrentJob(ctx, request.ID)
	} else {
		before, err = reader.Current(ctx, request.ID)
	}
	if err != nil {
		return WorkflowSnapshot{}, fmt.Errorf("read Pegasus cancellation: %w", err)
	}
	version := request.Version
	if request.ByJob {
		if !matchesCancellationJob(before, request) {
			return WorkflowSnapshot{}, ErrNotCancellable
		}
		if request.Version != before.JobVersion || request.Version < 1 {
			return WorkflowSnapshot{}, ErrVersionConflict
		}
		version = before.Summary.Version
	}
	if !canCancel(before, version) {
		return WorkflowSnapshot{}, ErrNotCancellable
	}
	return before, nil
}

func (service *WorkflowControl) cancellationPlan(
	before WorkflowSnapshot,
	request cancellationRequest,
) (CancellationPlan, error) {
	auditID, err := uuid.NewV7()
	if err != nil {
		return CancellationPlan{}, fmt.Errorf("generate Pegasus cancellation audit: %w", err)
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

func canCancel(before WorkflowSnapshot, version int64) bool {
	if !validWorkflowVersion(before, version) || (before.JobState != "QUEUED" && before.JobState != "RUNNING") {
		return false
	}
	if before.Summary.ImportJobID == nil {
		return before.Summary.ScanJobID != "" && before.Summary.State == "SCANNING"
	}
	return before.Summary.State == "QUEUED" || before.Summary.State == "RUNNING"
}

func matchesCancellationJob(before WorkflowSnapshot, request cancellationRequest) bool {
	if before.Summary.ID != request.ScopeID {
		return false
	}
	if before.Summary.ImportJobID != nil {
		return *before.Summary.ImportJobID == request.ID && request.Kind == "SERVER_PEGASUS_IMPORT"
	}
	return before.Summary.ScanJobID == request.ID && request.Kind == "SERVER_PEGASUS_SCAN"
}
