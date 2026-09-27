package cleanupjobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"

	"github.com/google/uuid"
)

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

func (service *Scheduler) Queue(ctx context.Context, scope JobWriter, request ScheduleRequest) (string, error) {
	if !validScheduleScope(request.Scope.Type) || request.Scope.ID == "" || request.ScopeVersion < 1 ||
		!ValidReason(request.Reason) || request.NowMS < 0 {
		return "", ErrScopeInvalid
	}
	job, err := service.prepare(request)
	if err != nil {
		return "", err
	}
	if err := scope.CreateJob(ctx, job); err != nil {
		return "", fmt.Errorf("persist release schedule: %w", err)
	}
	return job.ID, nil
}

func (service *Scheduler) prepare(request ScheduleRequest) (ScheduledJob, error) {
	jobID, err := service.identity()
	if err != nil {
		return ScheduledJob{}, err
	}
	executionID, err := service.identity()
	if err != nil {
		return ScheduledJob{}, err
	}
	input := Input{
		SchemaVersion: 1, Kind: "OWNER_CLEANUP", Scope: request.Scope, ExecutionID: executionID,
		Inputs: ScopeInputs{ScopeVersion: request.ScopeVersion, Reason: request.Reason},
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return ScheduledJob{}, fmt.Errorf("encode release input: %w", err)
	}
	inputDigest := sha256.Sum256(encoded)
	dedupe := sha256.Sum256([]byte("retrom-job-dedupe-v1\x00OWNER_CLEANUP\x00" +
		string(request.Scope.Type) + "\x00" + request.Scope.ID))
	return ScheduledJob{
		ID:    jobID,
		Scope: request.Scope,
		NowMS: request.NowMS,

		DedupeKey:   hex.EncodeToString(dedupe[:]),
		InputJSON:   string(encoded),
		InputDigest: hex.EncodeToString(inputDigest[:]),
	}, nil
}

func (service *Scheduler) identity() (string, error) {
	id, err := service.newID()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrScheduleIDInvalid, err)
	}
	if id == "" {
		return "", ErrScheduleIDInvalid
	}
	return id, nil
}

func (service *Scheduler) ScheduleOwner(
	ctx context.Context, scope OwnerSchedulingScope, owner Owner, reason Reason, now int64,
) (string, error) {
	if owner.PayloadState != "RETAINED" {
		return ExistingOwnerRelease(owner)
	}
	if owner.Version == math.MaxInt64 {
		return "", ErrScopeInvalid
	}
	id, err := service.Queue(ctx, scope, ScheduleRequest{
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

func ReadSchedulingOwner(ctx context.Context, scope OwnerSchedulingScope, ref Scope) (Owner, error) {
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

func ExistingOwnerRelease(owner Owner) (string, error) {
	if owner.ReleaseJobID != "" && (owner.PayloadState == "RELEASING" || owner.PayloadState == "RELEASED") {
		return owner.ReleaseJobID, nil
	}
	return "", ErrScopeInvalid
}
