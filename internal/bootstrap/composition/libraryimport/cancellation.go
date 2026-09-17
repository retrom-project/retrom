package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	jobsmodel "retrom/internal/model/jobs"
	libraryimportmodel "retrom/internal/model/libraryimport"
	repository "retrom/internal/repo/libraryimport"
	jobsservice "retrom/internal/service/jobs"
	libraryimportservice "retrom/internal/service/libraryimport"
)

func WithJobCancellation(service *jobsservice.Service, database *sql.DB, now func() time.Time) *jobsservice.Service {
	return service.WithDomainCancellation(map[string]jobsservice.DomainCanceller{"IMPORT_GROUP": importCancellation{
		executions: libraryimportservice.NewImportExecutions(repository.NewImportExecutions(database), now),
	}})
}

// NewImportBatchCancellations composes the aggregate cancellation use case
// with the persistence adapter. The legacy libraryimport facade uses this
// constructor while callers migrate to the application port.
func NewImportBatchCancellations(
	database *sql.DB,
	now func() time.Time,
) *libraryimportservice.ImportBatchCancellations {
	return libraryimportservice.NewImportBatchCancellations(repository.NewImportBatchCancellations(database), now)
}

type importCancellation struct {
	executions *libraryimportservice.ImportExecutions
}

func (handler importCancellation) CancelJob(
	ctx context.Context,
	request jobsservice.DomainCancellation,
) (jobsmodel.Result, bool, error) {
	if request.Kind != "IMPORT_GROUP" {
		return jobsmodel.Result{}, false, jobsmodel.ErrConflict
	}
	result, err := handler.executions.CancelJob(
		ctx,
		libraryimportmodel.ImportJobCancellation{
			JobID: request.JobID, ImportID: request.ScopeID,
			ExpectedVersion: request.ExpectedVersion, Reason: request.Reason,
		},
	)
	if err != nil {
		if errors.Is(err, libraryimportmodel.ErrInvalid) || errors.Is(err, libraryimportmodel.ErrVersionConflict) {
			return jobsmodel.Result{}, false, fmt.Errorf("%w: %w", jobsmodel.ErrConflict, err)
		}
		return jobsmodel.Result{}, false, fmt.Errorf("cancel ordinary import job: %w", err)
	}
	return jobsmodel.Result{
		Kind: "IMPORT_GROUP", JobID: result.JobID, State: result.State,
		ExecutionNo: result.ExecutionNo, Version: result.Version,
	}, result.Pending, nil
}
