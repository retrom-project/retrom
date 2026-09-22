package composition

import (
	"context"
	"errors"
	"fmt"

	"retrom/internal/authn"
	"retrom/internal/service/jobs"
	source "retrom/internal/service/sourceimport"
)

type SourceJobCanceller interface {
	CancelJob(context.Context, source.JobCancellationRequest) (source.JobCancellationResult, bool, error)
}

func WithSourceJobCancellation(service *jobs.Service, source SourceJobCanceller) *jobs.Service {
	handler := sourceCancellation{source: source}
	return service.WithDomainCancellation(map[string]jobs.DomainCanceller{
		"IMPORT_SCAN": handler, "IMPORT_RECEIVE": handler,
	})
}

type sourceCancellation struct{ source SourceJobCanceller }

func (handler sourceCancellation) CancelJob(
	ctx context.Context,
	command jobs.DomainCancellation,
) (jobs.Result, bool, error) {
	principal, ok := authn.PrincipalFromContext(ctx)
	if !ok || principal.UserID == "" {
		return jobs.Result{}, false, jobs.ErrConflict
	}
	result, pending, err := handler.source.CancelJob(ctx, source.JobCancellationRequest{
		JobID: command.JobID, Kind: command.Kind, ScopeID: command.ScopeID, ExpectedVersion: command.ExpectedVersion,
		Reason: command.Reason, ActorID: principal.UserID,
	})
	if err != nil {
		if errors.Is(err, source.ErrVersionConflict) || errors.Is(err, source.ErrNotCancellable) ||
			errors.Is(err, source.ErrNotFound) {
			return jobs.Result{}, false, fmt.Errorf("%w: %w", jobs.ErrConflict, err)
		}
		return jobs.Result{}, false, fmt.Errorf("cancel Source domain job: %w", err)
	}
	return jobs.Result{
		JobID: result.JobID, State: result.State, ExecutionNo: result.ExecutionNo, Version: result.Version,
	}, pending, nil
}
