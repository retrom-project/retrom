package composition

import (
	"context"
	"errors"
	"fmt"

	"retrom/internal/capability/security/authn"
	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	jobsmodel "retrom/internal/model/jobs"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
	jobsservice "retrom/internal/service/jobs"
)

type EmulationStationJobCanceller interface {
	CancelJob(
		context.Context,
		emulationstationimportservice.JobCancellationRequest,
	) (emulationstationimportservice.JobCancellationResult, bool, error)
}

func WithEmulationStationJobCancellation(
	service *jobsservice.Service,
	source EmulationStationJobCanceller,
) *jobsservice.Service {
	handler := emulationStationCancellation{source: source}
	return service.WithDomainCancellation(map[string]jobsservice.DomainCanceller{
		"SERVER_EMULATIONSTATION_SCAN": handler, "SERVER_EMULATIONSTATION_IMPORT": handler,
	})
}

type emulationStationCancellation struct{ source EmulationStationJobCanceller }

func (handler emulationStationCancellation) CancelJob(
	ctx context.Context, command jobsservice.DomainCancellation,
) (jobsmodel.Result, bool, error) {
	principal, ok := authn.PrincipalFromContext(ctx)
	if !ok || principal.UserID == "" {
		return jobsmodel.Result{}, false, jobsmodel.ErrConflict
	}
	result, pending, err := handler.source.CancelJob(ctx, emulationstationimportservice.JobCancellationRequest{
		JobID: command.JobID, Kind: command.Kind, ScopeID: command.ScopeID, Reason: command.Reason,
		ActorID: principal.UserID, ExpectedVersion: command.ExpectedVersion,
	})
	if err != nil {
		return jobsmodel.Result{}, false, emulationStationCancellationError(err)
	}
	return jobsmodel.Result{
		Kind: command.Kind, JobID: result.JobID, State: result.State,
		ExecutionNo: result.ExecutionNo, Version: result.Version,
	}, pending, nil
}

func emulationStationCancellationError(err error) error {
	if errors.Is(err, emulationstationimportmodel.ErrVersionConflict) ||
		errors.Is(err, emulationstationimportmodel.ErrNotCancellable) ||
		errors.Is(err, emulationstationimportmodel.ErrNotFound) {
		return fmt.Errorf("%w: %w", jobsmodel.ErrConflict, err)
	}
	return fmt.Errorf("cancel EmulationStation domain job: %w", err)
}
