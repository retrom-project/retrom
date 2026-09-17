package pegasusimport

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/pegasusimport"

	"github.com/google/uuid"
)

type WorkflowControl struct {
	repository model.WorkflowRepository
	now        func() time.Time
}

func NewWorkflowControl(repository model.WorkflowRepository, now func() time.Time) *WorkflowControl {
	return &WorkflowControl{repository: repository, now: now}
}

func (service *WorkflowControl) Retry(
	ctx context.Context,
	id string,
	version int64,
	actorID string,
) (model.Summary, error) {
	executionID, err := uuid.NewV7()
	if err != nil {
		return model.Summary{}, fmt.Errorf("generate Pegasus retry execution: %w", err)
	}
	auditID, err := uuid.NewV7()
	if err != nil {
		return model.Summary{}, fmt.Errorf("generate Pegasus retry audit: %w", err)
	}
	cmd := model.RetryWorkflowCommand{
		ID: id, ActorID: actorID, Version: version,
		NowMS:       service.now().UnixMilli(),
		ExecutionID: executionID.String(),
		AuditID:     auditID.String(),
	}
	result, err := service.repository.CommitRetryWorkflow(ctx, cmd)
	if err != nil {
		return model.Summary{}, fmt.Errorf("finish Pegasus retry: %w", err)
	}
	return result, nil
}
