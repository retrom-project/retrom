package composition

import (
	"context"
	"errors"
	"fmt"

	"retrom/internal/capability/security/authn"
	jobsmodel "retrom/internal/model/jobs"
	pegasusimportmodel "retrom/internal/model/pegasusimport"
	jobsservice "retrom/internal/service/jobs"
	pegasusimportservice "retrom/internal/service/pegasusimport"
)

type PegasusJobCanceller interface {
	CancelJob(context.Context, pegasusimportservice.JobCancellationRequest) (pegasusimportservice.JobCancellationResult, bool, error)
}

func WithPegasusJobCancellation(service *jobsservice.Service, source PegasusJobCanceller) *jobsservice.Service {
	handler := pegasusCancellation{source: source}
	return service.WithDomainCancellation(map[string]jobsservice.DomainCanceller{
		"SERVER_PEGASUS_SCAN": handler, "SERVER_PEGASUS_IMPORT": handler,
	})
}

type pegasusCancellation struct{ source PegasusJobCanceller }

func (handler pegasusCancellation) CancelJob(
	ctx context.Context,
	command jobsservice.DomainCancellation,
) (jobsmodel.Result, bool, error) {
	principal, ok := authn.PrincipalFromContext(ctx)
	if !ok || principal.UserID == "" {
		return jobsmodel.Result{}, false, jobsmodel.ErrConflict
	}
	result, pending, err := handler.source.CancelJob(ctx, pegasusimportservice.JobCancellationRequest{
		JobID: command.JobID, Kind: command.Kind, ScopeID: command.ScopeID, ExpectedVersion: command.ExpectedVersion,
		Reason: command.Reason, ActorID: principal.UserID,
	})
	if err != nil {
		if errors.Is(err, pegasusimportmodel.ErrVersionConflict) || errors.Is(err, pegasusimportmodel.ErrNotCancellable) ||
			errors.Is(err, pegasusimportmodel.ErrNotFound) {
			return jobsmodel.Result{}, false, fmt.Errorf("%w: %w", jobsmodel.ErrConflict, err)
		}
		return jobsmodel.Result{}, false, fmt.Errorf("cancel Pegasus domain job: %w", err)
	}
	return jobsmodel.Result{
		JobID: result.JobID, State: result.State, ExecutionNo: result.ExecutionNo, Version: result.Version,
	}, pending, nil
}
