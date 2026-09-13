package composition

import (
	"context"
	"errors"
	"fmt"

	"retrom/internal/capability/security/authn"
	es "retrom/internal/service/emulationstationimport"
	"retrom/internal/service/jobs"
)

type EmulationStationJobCanceller interface {
	CancelJob(context.Context, es.JobCancellationRequest) (es.JobCancellationResult, bool, error)
}

func WithEmulationStationJobCancellation(service *jobs.Service, source EmulationStationJobCanceller) *jobs.Service {
	handler := emulationStationCancellation{source: source}
	return service.WithDomainCancellation(map[string]jobs.DomainCanceller{
		"SERVER_EMULATIONSTATION_SCAN": handler, "SERVER_EMULATIONSTATION_IMPORT": handler,
	})
}

type emulationStationCancellation struct{ source EmulationStationJobCanceller }

func (handler emulationStationCancellation) CancelJob(
	ctx context.Context, command jobs.DomainCancellation,
) (jobs.Result, bool, error) {
	principal, ok := authn.PrincipalFromContext(ctx)
	if !ok || principal.UserID == "" {
		return jobs.Result{}, false, jobs.ErrConflict
	}
	result, pending, err := handler.source.CancelJob(ctx, es.JobCancellationRequest{
		JobID: command.JobID, Kind: command.Kind, ScopeID: command.ScopeID, Reason: command.Reason,
		ActorID: principal.UserID, ExpectedVersion: command.ExpectedVersion,
	})
	if err != nil {
		return jobs.Result{}, false, emulationStationCancellationError(err)
	}
	return jobs.Result{
		Kind: command.Kind, JobID: result.JobID, State: result.State,
		ExecutionNo: result.ExecutionNo, Version: result.Version,
	}, pending, nil
}

func emulationStationCancellationError(err error) error {
	if errors.Is(err, es.ErrVersionConflict) || errors.Is(err, es.ErrNotCancellable) || errors.Is(err, es.ErrNotFound) {
		return fmt.Errorf("%w: %w", jobs.ErrConflict, err)
	}
	return fmt.Errorf("cancel EmulationStation domain job: %w", err)
}
