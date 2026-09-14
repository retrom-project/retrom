package payloadrelease

import (
	"context"
	"fmt"
	"math"

	application "retrom/internal/model/payloadrelease"

	"github.com/google/uuid"
)

// Scheduler is the persistence-side coordinator used only when a caller's
// repository already owns the surrounding transaction. It shares model values
// and pure policy with the application scheduler but never calls service code.
type Scheduler struct{ newID func() (string, error) }

func NewScheduler(newID func() (string, error)) *Scheduler {
	if newID == nil {
		newID = func() (string, error) {
			id, err := uuid.NewV7()
			if err != nil {
				return "", fmt.Errorf("create release identity: %w", err)
			}
			return id.String(), nil
		}
	}
	return &Scheduler{newID: newID}
}

func (scheduler *Scheduler) Queue(
	ctx context.Context, scope application.SchedulingScope, request application.ScheduleRequest,
) (string, error) {
	if !application.ValidScheduleScope(request.Scope.Type) || request.Scope.ID == "" || request.ScopeVersion < 1 ||
		!application.ValidReason(request.Reason) || request.NowMS < 0 {
		return "", application.ErrScopeInvalid
	}
	job, err := scheduler.prepare(request)
	if err != nil {
		return "", err
	}
	if err := scope.CreateJob(ctx, job); err != nil {
		return "", fmt.Errorf("persist release schedule: %w", err)
	}
	return job.ID, nil
}

func (scheduler *Scheduler) prepare(request application.ScheduleRequest) (application.ScheduledJob, error) {
	jobID, err := scheduler.identity()
	if err != nil {
		return application.ScheduledJob{}, err
	}
	executionID, err := scheduler.identity()
	if err != nil {
		return application.ScheduledJob{}, err
	}
	job, err := application.BuildScheduledJob(request, jobID, executionID)
	if err != nil {
		return application.ScheduledJob{}, fmt.Errorf("build release schedule: %w", err)
	}
	return job, nil
}

func (scheduler *Scheduler) identity() (string, error) {
	id, err := scheduler.newID()
	if err != nil {
		return "", fmt.Errorf("%w: %w", application.ErrScheduleIDInvalid, err)
	}
	if id == "" {
		return "", application.ErrScheduleIDInvalid
	}
	return id, nil
}

func (scheduler *Scheduler) TerminalItem(
	ctx context.Context, scope application.SchedulingScope, id string,
	reason application.Reason, now int64,
) (string, error) {
	owner, err := readSchedulingOwner(ctx, scope, application.Scope{Type: application.ScopeImportItem, ID: id})
	if err != nil {
		return "", err
	}
	if !application.TerminalImportItem(owner.State) {
		return "", application.ErrScopeInvalid
	}
	return scheduler.scheduleOwner(ctx, scope, owner, reason, now)
}

func (scheduler *Scheduler) TerminalImport(
	ctx context.Context, scope application.SchedulingScope, id string, now int64,
) (string, error) {
	pending, err := scope.PendingChildren(ctx, id)
	if err != nil {
		return "", fmt.Errorf("read pending release children: %w", err)
	}
	if pending > 0 {
		return "", nil
	}
	owner, err := readSchedulingOwner(ctx, scope, application.Scope{Type: application.ScopeImportJob, ID: id})
	if err != nil {
		return "", err
	}
	if !application.TerminalImportJob(owner.State) {
		return "", nil
	}
	return scheduler.scheduleOwner(ctx, scope, owner, application.ReasonImportTerminal, now)
}

func (scheduler *Scheduler) scheduleOwner(
	ctx context.Context, scope application.SchedulingScope, owner application.Owner,
	reason application.Reason, now int64,
) (string, error) {
	if owner.PayloadState != "RETAINED" {
		return existingOwnerRelease(owner)
	}
	if owner.Version == math.MaxInt64 {
		return "", application.ErrScopeInvalid
	}
	id, err := scheduler.Queue(ctx, scope, application.ScheduleRequest{
		Scope: owner.Scope, ScopeVersion: owner.Version + 1, Reason: reason, NowMS: now,
	})
	if err != nil {
		return "", err
	}
	if err := scope.BeginRelease(ctx, application.OwnerRelease{Before: owner, JobID: id, NowMS: now}); err != nil {
		return "", fmt.Errorf("begin owner payload release: %w", err)
	}
	return id, nil
}

func readSchedulingOwner(
	ctx context.Context, scope application.SchedulingScope, ref application.Scope,
) (application.Owner, error) {
	if ref.ID == "" {
		return application.Owner{}, application.ErrScopeInvalid
	}
	owner, err := scope.Owner(ctx, ref)
	if err != nil {
		return application.Owner{}, fmt.Errorf("read release owner: %w", err)
	}
	if owner.Scope != ref || owner.Version < 1 {
		return application.Owner{}, application.ErrScopeInvalid
	}
	return owner, nil
}

func existingOwnerRelease(owner application.Owner) (string, error) {
	if owner.ReleaseJobID != "" && (owner.PayloadState == "RELEASING" || owner.PayloadState == "RELEASED" ||
		owner.PayloadState == "FAILED") {
		return owner.ReleaseJobID, nil
	}
	return "", application.ErrScopeInvalid
}

func (scheduler *Scheduler) TerminalSource(
	ctx context.Context, scope application.SchedulingScope, ref application.Scope, now int64,
) (string, error) {
	if ref.Type != application.ScopePegasusImportItem && ref.Type != application.ScopeEmulationStationImportItem {
		return "", application.ErrScopeInvalid
	}
	owner, err := readSchedulingOwner(ctx, scope, ref)
	if err != nil {
		return "", err
	}
	if !application.TerminalSourceItem(owner.State, owner.Retryable) {
		return "", nil
	}
	if owner.PayloadState != "RETAINED" {
		return existingOwnerRelease(owner)
	}
	if owner.PublicID != "" {
		return scheduler.linkSource(ctx, scope, owner, now)
	}
	reason := application.ReasonPegasusTerminal
	if ref.Type == application.ScopeEmulationStationImportItem {
		reason = application.ReasonEmulationStationTerminal
	}
	return scheduler.scheduleOwner(ctx, scope, owner, reason, now)
}

func (scheduler *Scheduler) linkSource(
	ctx context.Context, scope application.SchedulingScope, owner application.Owner, now int64,
) (string, error) {
	if owner.Version == math.MaxInt64 {
		return "", application.ErrScopeInvalid
	}
	ordinary, err := readSchedulingOwner(
		ctx, scope, application.Scope{Type: application.ScopeImportItem, ID: owner.PublicID},
	)
	if err != nil {
		return "", err
	}
	jobID, err := existingOwnerRelease(ordinary)
	if err != nil {
		return "", err
	}
	if err := scope.BeginRelease(ctx, application.OwnerRelease{Before: owner, JobID: jobID, NowMS: now}); err != nil {
		return "", fmt.Errorf("link source to ordinary payload release: %w", err)
	}
	return jobID, nil
}

func (scheduler *Scheduler) Consumption(
	ctx context.Context, scope application.SchedulingScope, id string, now int64,
) (string, error) {
	before, err := scope.Consumption(ctx, id)
	if err != nil {
		return "", fmt.Errorf("read upload consumption release: %w", err)
	}
	if before.Released {
		return "", nil
	}
	if before.ExistingJobID != "" {
		return before.ExistingJobID, nil
	}
	return scheduler.Queue(ctx, scope, application.ScheduleRequest{
		Scope: application.Scope{Type: application.ScopeUploadConsumption, ID: id}, ScopeVersion: before.Version,
		Reason: application.ReasonUploadConsumed, NowMS: now,
	})
}

func (scheduler *Scheduler) DeleteGame(
	ctx context.Context, scope application.SchedulingScope, id string, version, now int64,
) (string, error) {
	before, err := readSchedulingOwner(ctx, scope, application.Scope{Type: application.ScopeGame, ID: id})
	if err != nil {
		return "", err
	}
	if before.Version != version || version == math.MaxInt64 || before.State != "PUBLISHED" ||
		before.PayloadState != "RETAINED" {
		return "", application.ErrScopeInvalid
	}
	jobID, err := scheduler.Queue(ctx, scope, application.ScheduleRequest{
		Scope: before.Scope, ScopeVersion: version + 1, Reason: application.ReasonGameDeleted, NowMS: now,
	})
	if err != nil {
		return "", err
	}
	if err := scope.BeginRelease(ctx, application.OwnerRelease{
		Before: before, JobID: jobID, NowMS: now, DeleteGame: true,
	}); err != nil {
		return "", fmt.Errorf("schedule deleted game payload: %w", err)
	}
	return jobID, nil
}
