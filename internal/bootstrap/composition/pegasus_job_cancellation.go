package composition

import (
	"context"
	"errors"
	"fmt"

	"retrom/internal/capability/security/authn"
	"retrom/internal/service/jobs"
	pegasus "retrom/internal/service/pegasusimport"
)

type PegasusJobCanceller interface {
	CancelJob(context.Context, pegasus.JobCancellationRequest) (pegasus.JobCancellationResult, bool, error)
}

func WithPegasusJobCancellation(service *jobs.Service, source PegasusJobCanceller) *jobs.Service {
	handler := pegasusCancellation{source: source}
	return service.WithDomainCancellation(map[string]jobs.DomainCanceller{
		"SERVER_PEGASUS_SCAN": handler, "SERVER_PEGASUS_IMPORT": handler,
	})
}

type pegasusCancellation struct{ source PegasusJobCanceller }

func (handler pegasusCancellation) CancelJob(
	ctx context.Context,
	command jobs.DomainCancellation,
) (jobs.Result, bool, error) {
	principal, ok := authn.PrincipalFromContext(ctx)
	if !ok || principal.UserID == "" {
		return jobs.Result{}, false, jobs.ErrConflict
	}
	result, pending, err := handler.source.CancelJob(ctx, pegasus.JobCancellationRequest{
		JobID: command.JobID, Kind: command.Kind, ScopeID: command.ScopeID, ExpectedVersion: command.ExpectedVersion,
		Reason: command.Reason, ActorID: principal.UserID,
	})
	if err != nil {
		if errors.Is(err, pegasus.ErrVersionConflict) || errors.Is(err, pegasus.ErrNotCancellable) ||
			errors.Is(err, pegasus.ErrNotFound) {
			return jobs.Result{}, false, fmt.Errorf("%w: %w", jobs.ErrConflict, err)
		}
		return jobs.Result{}, false, fmt.Errorf("cancel Pegasus domain job: %w", err)
	}
	return jobs.Result{
		JobID: result.JobID, State: result.State, ExecutionNo: result.ExecutionNo, Version: result.Version,
	}, pending, nil
}
