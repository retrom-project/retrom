package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	repository "retrom/internal/persistence/libraryimport"
	"retrom/internal/service/jobs"
	application "retrom/internal/service/libraryimport"
)

func WithJobCancellation(service *jobs.Service, database *sql.DB, now func() time.Time) *jobs.Service {
	return service.WithDomainCancellation(map[string]jobs.DomainCanceller{"IMPORT_GROUP": importCancellation{
		executions: application.NewImportExecutions(repository.NewImportExecutions(database), now),
	}})
}

type importCancellation struct{ executions *application.ImportExecutions }

func (handler importCancellation) CancelJob(
	ctx context.Context,
	request jobs.DomainCancellation,
) (jobs.Result, bool, error) {
	if request.Kind != "IMPORT_GROUP" {
		return jobs.Result{}, false, jobs.ErrConflict
	}
	result, err := handler.executions.CancelJob(
		ctx,
		application.ImportJobCancellation{
			JobID:           request.JobID,
			ImportID:        request.ScopeID,
			ExpectedVersion: request.ExpectedVersion,
			Reason:          request.Reason,
		},
	)
	if err != nil {
		if errors.Is(err, application.ErrInvalid) || errors.Is(err, application.ErrVersionConflict) {
			return jobs.Result{}, false, fmt.Errorf("%w: %w", jobs.ErrConflict, err)
		}
		return jobs.Result{}, false, fmt.Errorf("cancel ordinary import job: %w", err)
	}
	return jobs.Result{
		Kind:        "IMPORT_GROUP",
		JobID:       result.JobID,
		State:       result.State,
		ExecutionNo: result.ExecutionNo,
		Version:     result.Version,
	}, result.Pending, nil
}
