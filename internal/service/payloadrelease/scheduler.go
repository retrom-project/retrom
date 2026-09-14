package payloadrelease

import (
	"context"
	"fmt"
	"math"

	application "retrom/internal/model/payloadrelease"

	"github.com/google/uuid"
)

// Scheduler coordinates payload-release use cases. The model package keeps
// only the value types and pure state policy; all storage reads and writes are
// coordinated here through the shared ports.
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

func (scheduler *Scheduler) Queue(ctx context.Context, scope SchedulingScope, request ScheduleRequest) (string, error) {
	if !ValidScheduleScope(request.Scope.Type) || request.Scope.ID == "" || request.ScopeVersion < 1 ||
		!ValidReason(request.Reason) || request.NowMS < 0 {
		return "", ErrScopeInvalid
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

func (scheduler *Scheduler) prepare(request ScheduleRequest) (ScheduledJob, error) {
	jobID, err := scheduler.identity()
	if err != nil {
		return ScheduledJob{}, err
	}
	executionID, err := scheduler.identity()
	if err != nil {
		return ScheduledJob{}, err
	}
	return application.BuildScheduledJob(request, jobID, executionID)
}

func (scheduler *Scheduler) identity() (string, error) {
	id, err := scheduler.newID()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrScheduleIDInvalid, err)
	}
	if id == "" {
		return "", ErrScheduleIDInvalid
	}
	return id, nil
}

func (scheduler *Scheduler) Identity() (string, error) { return scheduler.identity() }

func (scheduler *Scheduler) TerminalItem(
	ctx context.Context, scope SchedulingScope, id string, reason Reason, now int64,
) (string, error) {
	owner, err := readSchedulingOwner(ctx, scope, Scope{Type: ScopeImportItem, ID: id})
	if err != nil {
		return "", err
	}
	if !TerminalImportItem(owner.State) {
		return "", ErrScopeInvalid
	}
	return scheduler.scheduleOwner(ctx, scope, owner, reason, now)
}

func (scheduler *Scheduler) TerminalImport(
	ctx context.Context, scope SchedulingScope, id string, now int64,
) (string, error) {
	pending, err := scope.PendingChildren(ctx, id)
	if err != nil {
		return "", fmt.Errorf("read pending release children: %w", err)
	}
	if pending > 0 {
		return "", nil
	}
	owner, err := readSchedulingOwner(ctx, scope, Scope{Type: ScopeImportJob, ID: id})
	if err != nil {
		return "", err
	}
	if !TerminalImportJob(owner.State) {
		return "", nil
	}
	return scheduler.scheduleOwner(ctx, scope, owner, ReasonImportTerminal, now)
}

func (scheduler *Scheduler) scheduleOwner(
	ctx context.Context, scope SchedulingScope, owner Owner, reason Reason, now int64,
) (string, error) {
	if owner.PayloadState != "RETAINED" {
		return existingOwnerRelease(owner)
	}
	if owner.Version == math.MaxInt64 {
		return "", ErrScopeInvalid
	}
	id, err := scheduler.Queue(ctx, scope, ScheduleRequest{
		Scope: owner.Scope, ScopeVersion: owner.Version + 1, Reason: reason, NowMS: now,
	})
	if err != nil {
		return "", err
	}
	if err := scope.BeginRelease(ctx, OwnerRelease{Before: owner, JobID: id, NowMS: now}); err != nil {
		return "", fmt.Errorf("begin owner payload release: %w", err)
	}
	return id, nil
}

func readSchedulingOwner(ctx context.Context, scope SchedulingScope, ref Scope) (Owner, error) {
	if ref.ID == "" {
		return Owner{}, ErrScopeInvalid
	}
	owner, err := scope.Owner(ctx, ref)
	if err != nil {
		return Owner{}, fmt.Errorf("read release owner: %w", err)
	}
	if owner.Scope != ref || owner.Version < 1 {
		return Owner{}, ErrScopeInvalid
	}
	return owner, nil
}

func existingOwnerRelease(owner Owner) (string, error) {
	if owner.ReleaseJobID != "" && (owner.PayloadState == "RELEASING" || owner.PayloadState == "RELEASED" ||
		owner.PayloadState == "FAILED") {
		return owner.ReleaseJobID, nil
	}
	return "", ErrScopeInvalid
}

func (scheduler *Scheduler) TerminalSource(
	ctx context.Context, scope SchedulingScope, ref Scope, now int64,
) (string, error) {
	if ref.Type != ScopePegasusImportItem && ref.Type != ScopeEmulationStationImportItem {
		return "", ErrScopeInvalid
	}
	owner, err := readSchedulingOwner(ctx, scope, ref)
	if err != nil {
		return "", err
	}
	if !TerminalSourceItem(owner.State, owner.Retryable) {
		return "", nil
	}
	if owner.PayloadState != "RETAINED" {
		return existingOwnerRelease(owner)
	}
	if owner.PublicID != "" {
		return scheduler.linkSource(ctx, scope, owner, now)
	}
	reason := ReasonPegasusTerminal
	if ref.Type == ScopeEmulationStationImportItem {
		reason = ReasonEmulationStationTerminal
	}
	return scheduler.scheduleOwner(ctx, scope, owner, reason, now)
}

func (scheduler *Scheduler) linkSource(
	ctx context.Context, scope SchedulingScope, owner Owner, now int64,
) (string, error) {
	if owner.Version == math.MaxInt64 {
		return "", ErrScopeInvalid
	}
	ordinary, err := readSchedulingOwner(ctx, scope, Scope{Type: ScopeImportItem, ID: owner.PublicID})
	if err != nil {
		return "", err
	}
	jobID, err := existingOwnerRelease(ordinary)
	if err != nil {
		return "", err
	}
	if err := scope.BeginRelease(ctx, OwnerRelease{Before: owner, JobID: jobID, NowMS: now}); err != nil {
		return "", fmt.Errorf("link source to ordinary payload release: %w", err)
	}
	return jobID, nil
}

func (scheduler *Scheduler) Consumption(
	ctx context.Context, scope SchedulingScope, id string, now int64,
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
	return scheduler.Queue(ctx, scope, ScheduleRequest{
		Scope: Scope{Type: ScopeUploadConsumption, ID: id}, ScopeVersion: before.Version,
		Reason: ReasonUploadConsumed, NowMS: now,
	})
}

func (scheduler *Scheduler) DeleteGame(
	ctx context.Context, scope SchedulingScope, id string, version, now int64,
) (string, error) {
	before, err := readSchedulingOwner(ctx, scope, Scope{Type: ScopeGame, ID: id})
	if err != nil {
		return "", err
	}
	if before.Version != version || version == math.MaxInt64 || before.State != "PUBLISHED" ||
		before.PayloadState != "RETAINED" {
		return "", ErrScopeInvalid
	}
	jobID, err := scheduler.Queue(ctx, scope, ScheduleRequest{
		Scope: before.Scope, ScopeVersion: version + 1, Reason: ReasonGameDeleted, NowMS: now,
	})
	if err != nil {
		return "", err
	}
	if err := scope.BeginRelease(ctx, OwnerRelease{Before: before, JobID: jobID, NowMS: now, DeleteGame: true}); err != nil {
		return "", fmt.Errorf("schedule deleted game payload: %w", err)
	}
	return jobID, nil
}

func (scheduler *Scheduler) Review(ctx context.Context, scope ReleaseScope, request ReviewRelease) error {
	if request.ItemID == "" || request.ImportID == "" || !ValidReason(request.Reason) || request.NowMS < 0 {
		return ErrScopeInvalid
	}
	if _, err := scheduler.TerminalItem(ctx, scope.Scheduling, request.ItemID, request.Reason, request.NowMS); err != nil {
		return err
	}
	if err := scheduler.boundSources(ctx, scope, request.ItemID, request.NowMS); err != nil {
		return err
	}
	if _, err := scheduler.TerminalImport(ctx, scope.Scheduling, request.ImportID, request.NowMS); err != nil {
		return err
	}
	return nil
}

func (scheduler *Scheduler) boundSources(ctx context.Context, scope ReleaseScope, itemID string, now int64) error {
	var cursor Scope
	for {
		sources, err := scope.Links.BoundSources(ctx, itemID, cursor, 200)
		if err != nil {
			return fmt.Errorf("read bound source release owners: %w", err)
		}
		if len(sources) == 0 {
			return nil
		}
		for _, source := range sources {
			if source.Type < cursor.Type || source.Type == cursor.Type && source.ID <= cursor.ID {
				return ErrScopeInvalid
			}
			if _, err := scheduler.TerminalSource(ctx, scope.Scheduling, source, now); err != nil {
				return err
			}
			cursor = source
		}
	}
}

func (scheduler *Scheduler) TerminalSources(ctx context.Context, scope ReleaseScope, batch SourceBatch, now int64) error {
	if batch.ImportID == "" || !isSourceItemScope(batch.Type) || now < 0 {
		return ErrScopeInvalid
	}
	cursor := ""
	for {
		ids, err := scope.Links.RetainedSources(ctx, batch, cursor, 200)
		if err != nil {
			return fmt.Errorf("read source payload owners: %w", err)
		}
		if len(ids) == 0 {
			return nil
		}
		for _, id := range ids {
			if id <= cursor {
				return ErrScopeInvalid
			}
			if _, err := scheduler.TerminalSource(ctx, scope.Scheduling, Scope{Type: batch.Type, ID: id}, now); err != nil {
				return err
			}
			cursor = id
		}
	}
}

func isSourceItemScope(kind ScopeType) bool {
	return kind == ScopePegasusImportItem || kind == ScopeEmulationStationImportItem
}
