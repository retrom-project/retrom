package emulationstationimport

import (
	"context"
	"fmt"
	"math"
	"time"

	model "retrom/internal/model/emulationstationimport"

	"github.com/google/uuid"
)

func (service *Leases) Claim(ctx context.Context) (model.Execution, bool, error) {
	now := service.now().UnixMilli()
	before, exists, err := service.repository.LoadLeaseCandidate(ctx, now)
	if err != nil {
		return model.Execution{}, false, fmt.Errorf(
			"claim EmulationStation execution: read EmulationStation lease candidate: %w", err,
		)
	}
	if !exists {
		return model.Execution{}, false, nil
	}
	change, err := planLease(before, now)
	if err != nil {
		return model.Execution{}, false, fmt.Errorf("claim EmulationStation execution: %w", err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return model.Execution{}, false, fmt.Errorf(
			"claim EmulationStation execution: generate EmulationStation worker identity: %w", err,
		)
	}
	change.Execution.WorkerID = id.String()
	if err := service.repository.CommitLeaseClaim(ctx, change); err != nil {
		return model.Execution{}, false, fmt.Errorf("claim EmulationStation execution: %w", err)
	}
	return change.Execution, true, nil
}

func planLease(before model.LeaseSnapshot, now int64) (model.ClaimLease, error) {
	if !validLeaseCandidate(before, now) {
		return model.ClaimLease{}, model.ErrInvalid
	}
	change := model.ClaimLease{
		Before: before, Execution: before.Execution, ImportState: "SCANNING",
		Phase: "DISCOVERING_GAMELISTS", NowMS: now, StartedAtMS: now,
	}
	switch before.Kind {
	case "SERVER_EMULATIONSTATION_SCAN":
		if before.ImportState != "SCANNING" {
			return model.ClaimLease{}, model.ErrVersionConflict
		}
	case "SERVER_EMULATIONSTATION_IMPORT":
		if before.ImportState != "QUEUED" {
			return model.ClaimLease{}, model.ErrVersionConflict
		}
		change.ImportState, change.Phase = "RUNNING", "COPYING_CONTENT"
	default:
		return model.ClaimLease{}, model.ErrInvalid
	}
	if (before.StartedAtMS == nil) != (before.DeadlineAtMS == 0) {
		return model.ClaimLease{}, model.ErrInvalid
	}
	change.Execution.DeadlineAtMS = now + (8 * time.Hour).Milliseconds()
	if before.DeadlineAtMS != 0 {
		if before.DeadlineAtMS <= now {
			return model.ClaimLease{}, model.ErrExpired
		}
		change.StartedAtMS, change.Execution.DeadlineAtMS = *before.StartedAtMS, before.DeadlineAtMS
	}
	change.Execution.Attempt++
	change.UntilMS = min(now+60000, change.Execution.DeadlineAtMS)
	return change, nil
}

func validLeaseCandidate(before model.LeaseSnapshot, now int64) bool {
	return before.JobState == "QUEUED" && before.AvailableAtMS <= now && before.JobVersion > 0 &&
		before.JobVersion < math.MaxInt64 && before.ImportVersion > 0 && before.ImportVersion < math.MaxInt64 &&
		before.ExecutionNo > 0 && before.Attempt >= 0 && before.Attempt < before.MaxAttempts &&
		now <= math.MaxInt64-(8*time.Hour).Milliseconds()
}

func (service *Leases) Renew(ctx context.Context, unit model.Execution) (model.LeaseState, error) {
	before, found, err := service.repository.LoadCurrentLease(ctx, unit.JobID)
	if err != nil {
		return model.LeaseLost, fmt.Errorf(
			"renew EmulationStation execution: read EmulationStation lease: %w", err,
		)
	}
	if !found {
		return model.LeaseLost, nil
	}
	now := service.now().UnixMilli()
	state := ExecutionState(before, unit, now)
	if state != model.LeaseActive {
		return state, nil
	}
	if now > math.MaxInt64-60000 {
		return model.LeaseLost, model.ErrInvalid
	}
	err = service.repository.CommitLeaseRenewal(
		ctx,
		model.RenewLease{Before: before, NowMS: now, UntilMS: min(now+60000, before.DeadlineAtMS)},
	)
	if err != nil {
		return model.LeaseLost, fmt.Errorf("renew EmulationStation execution: %w", err)
	}
	return state, nil
}

// ExecutionState delegates to the model-level pure function.
func ExecutionState(before model.LeaseSnapshot, unit model.Execution, now int64) model.LeaseState {
	return model.ExecutionState(before, unit, now)
}
