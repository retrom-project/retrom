package libraryimport

import (
	"context"
	"errors"
	"fmt"
	"time"

	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/libraryimport"
	"retrom/internal/service/jobs"
	libraryservice "retrom/internal/service/libraryimport"
)

func WithJobCancellation(service *jobs.Service, executions *libraryservice.ImportExecutions) *jobs.Service {
	return service.WithDomainCancellation(map[string]jobs.DomainCanceller{"IMPORT_GROUP": importCancellation{
		executions: executions,
	}})
}

func WithAttachmentCancellation(service *jobs.Service, executions *libraryservice.AttachmentExecutions) *jobs.Service {
	handler := attachmentCancellation{executions: executions}
	return service.WithDomainCancellation(map[string]jobs.DomainCanceller{
		"REVIEW_ARCADE_PARENT_VALIDATE": handler, "REVIEW_MULTI_DISC_VALIDATE": handler,
	})
}

type attachmentCancellation struct {
	executions *libraryservice.AttachmentExecutions
}

func (handler attachmentCancellation) CancelJob(
	ctx context.Context, request jobs.DomainCancellation,
) (jobs.Result, bool, error) {
	result, err := handler.executions.CancelJob(ctx, libraryservice.ImportJobCancellation{
		JobID: request.JobID, ImportID: request.ScopeID, Reason: request.Reason,
	})
	if err != nil {
		if errors.Is(err, libraryservice.ErrInvalid) || errors.Is(err, libraryservice.ErrVersionConflict) {
			return jobs.Result{}, false, fmt.Errorf("%w: %w", jobs.ErrConflict, err)
		}
		return jobs.Result{}, false, fmt.Errorf("cancel attachment job: %w", err)
	}
	return jobs.Result{
		Kind: request.Kind, JobID: result.JobID, State: result.State,
		ExecutionNo: result.ExecutionNo, Version: result.Version,
	}, result.Pending, nil
}

// NewImportBatchCancellations binds the aggregate cancellation use case.
func NewImportBatchCancellations(database dbapi.DB, now func() time.Time) *libraryservice.ImportBatchCancellations {
	return libraryservice.NewImportBatchCancellations(repository.NewImportBatchCancellations(database), now)
}

type importCancellation struct {
	executions *libraryservice.ImportExecutions
}

func (handler importCancellation) CancelJob(
	ctx context.Context,
	request jobs.DomainCancellation,
) (jobs.Result, bool, error) {
	if request.Kind != "IMPORT_GROUP" {
		return jobs.Result{}, false, jobs.ErrConflict
	}
	result, err := handler.executions.CancelJob(
		ctx,
		libraryservice.ImportJobCancellation{
			JobID: request.JobID, ImportID: request.ScopeID,
			Reason: request.Reason,
		},
	)
	if err != nil {
		if errors.Is(err, libraryservice.ErrInvalid) || errors.Is(err, libraryservice.ErrVersionConflict) {
			return jobs.Result{}, false, fmt.Errorf("%w: %w", jobs.ErrConflict, err)
		}
		return jobs.Result{}, false, fmt.Errorf("cancel ordinary import job: %w", err)
	}
	return jobs.Result{
		Kind: "IMPORT_GROUP", JobID: result.JobID, State: result.State,
		ExecutionNo: result.ExecutionNo, Version: result.Version,
	}, result.Pending, nil
}
