package pegasusimport

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	model "retrom/internal/model/pegasusimport"

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
) (model.Summary, bool, error) {
	after, pending, err := service.cancel(
		ctx,
		cancellationRequest{ID: id, Version: version, Reason: reason, ActorID: actorID},
	)
	return after.Summary, pending, err
}

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
) (model.WorkflowSnapshot, bool, error) {
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || utf8.RuneCountInString(request.Reason) > 500 {
		return model.WorkflowSnapshot{}, false, model.ErrNotCancellable
	}
	audit, err := uuid.NewV7()
	if err != nil {
		return model.WorkflowSnapshot{}, false, fmt.Errorf("generate Pegasus cancellation audit: %w", err)
	}
	cmd := model.CancelWorkflowCommand{
		ID: request.ID, Reason: request.Reason, ActorID: request.ActorID,
		AuditID: audit.String(), Version: request.Version, NowMS: service.now().UnixMilli(),
		ByJob: request.ByJob, Kind: request.Kind, ScopeID: request.ScopeID,
	}
	result, pending, err := service.repository.CommitCancelWorkflow(ctx, cmd)
	if err != nil {
		return model.WorkflowSnapshot{}, false, fmt.Errorf("finish Pegasus cancellation: %w", err)
	}
	return result, pending, nil
}
