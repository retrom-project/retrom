package emulationstationimport

import (
	"context"
	"fmt"
	"strings"

	model "retrom/internal/model/emulationstationimport"

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
) (model.Summary, bool, error) {
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
) (model.WorkflowSnapshot, bool, error) {
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || len([]rune(request.Reason)) > 500 {
		return model.WorkflowSnapshot{}, false, model.ErrNotCancellable
	}
	audit, err := uuid.NewV7()
	if err != nil {
		return model.WorkflowSnapshot{}, false, fmt.Errorf("generate EmulationStation cancellation audit: %w", err)
	}
	cmd := model.CancelWorkflowCommand{
		ID: request.ID, Reason: request.Reason, ActorID: request.ActorID,
		AuditID: audit.String(), Version: request.Version, NowMS: service.now().UnixMilli(),
	}
	if request.Job != nil {
		cmd.Job = &model.CancelWorkflowJobInfo{
			JobID: request.Job.JobID, Kind: request.Job.Kind,
			ScopeID: request.Job.ScopeID, ExpectedVersion: request.Job.ExpectedVersion,
		}
	}
	result, pending, err := service.repository.CommitCancelWorkflow(ctx, cmd)
	if err != nil {
		return model.WorkflowSnapshot{}, false, fmt.Errorf("finish EmulationStation cancellation: %w", err)
	}
	return result, pending, nil
}
