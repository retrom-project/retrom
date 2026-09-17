package uploads

import (
	"context"
	"fmt"

	model "retrom/internal/model/uploads"

	"retrom/internal/foundation/cleanup"

	"github.com/google/uuid"
)

func (service *Service) Complete(ctx context.Context, id string, version int64) (string, int64, error) {
	jobID, err := uuid.NewV7()
	if err != nil {
		return "", 0, fmt.Errorf("generate finalization job ID: %w", err)
	}
	executionID, err := uuid.NewV7()
	if err != nil {
		return "", 0, fmt.Errorf("generate finalization execution ID: %w", err)
	}
	run, err := service.repository.CommitComplete(ctx, model.CompleteCommand{
		UploadID: id, Version: version, JobID: jobID.String(),
		ExecutionID: executionID.String(), NowMS: service.now().UnixMilli(),
	})
	if err != nil {
		return "", 0, fmt.Errorf("complete upload: %w", err)
	}
	service.Resume(ctx, run.JobID)
	return run.JobID, run.FinalizationNo, nil
}

func (service *Service) Cancel(ctx context.Context, id string, version int64) (model.Canceled, bool, error) {
	now := service.now().UnixMilli()
	result, err := service.repository.CommitCancel(ctx, model.CancelCommand{
		UploadID: id, Version: version, NowMS: now,
	})
	if err != nil {
		return model.Canceled{}, false, fmt.Errorf("cancel upload: %w", err)
	}
	if !result.Pending {
		cleanup.Error("remove cancelled upload parts", service.cleanupUpload(ctx, id, ""))
	}
	return result.Result, result.Pending, nil
}
