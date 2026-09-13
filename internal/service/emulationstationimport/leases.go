package emulationstationimport

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
)

func (service *Leases) Claim(ctx context.Context) (Execution, bool, error) {
	var unit Execution
	found := false
	err := service.repository.WithLease(ctx, func(scope LeaseScope) error {
		now := service.now().UnixMilli()
		before, exists, err := scope.Read.Next(ctx, now)
		if err != nil {
			return fmt.Errorf("read EmulationStation lease candidate: %w", err)
		}
		if !exists {
			return nil
		}
		change, err := planLease(before, now)
		if err != nil {
			return err
		}
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("generate EmulationStation worker identity: %w", err)
		}
		change.Execution.WorkerID = id.String()
		if err := scope.Write.Claim(ctx, change); err != nil {
			return fmt.Errorf("persist EmulationStation claim: %w", err)
		}
		unit, found = change.Execution, true
		return nil
	})
	if err != nil {
		return Execution{}, false, fmt.Errorf("claim EmulationStation execution: %w", err)
	}
	return unit, found, nil
}

func planLease(before LeaseSnapshot, now int64) (ClaimLease, error) {
	if !validLeaseCandidate(before, now) {
		return ClaimLease{}, ErrInvalid
	}
	change := ClaimLease{
		Before: before, Execution: before.Execution, ImportState: "SCANNING",
		Phase: "DISCOVERING_GAMELISTS", NowMS: now, StartedAtMS: now,
	}
	switch before.Kind {
	case "SERVER_EMULATIONSTATION_SCAN":
		if before.ImportState != "SCANNING" {
			return ClaimLease{}, ErrVersionConflict
		}
	case "SERVER_EMULATIONSTATION_IMPORT":
		if before.ImportState != "QUEUED" {
			return ClaimLease{}, ErrVersionConflict
		}
		change.ImportState, change.Phase = "RUNNING", "COPYING_CONTENT"
	default:
		return ClaimLease{}, ErrInvalid
	}
	if (before.StartedAtMS == nil) != (before.DeadlineAtMS == 0) {
		return ClaimLease{}, ErrInvalid
	}
	change.Execution.DeadlineAtMS = now + (8 * time.Hour).Milliseconds()
	if before.DeadlineAtMS != 0 {
		if before.DeadlineAtMS <= now {
			return ClaimLease{}, ErrExpired
		}
		change.StartedAtMS, change.Execution.DeadlineAtMS = *before.StartedAtMS, before.DeadlineAtMS
	}
	change.Execution.Attempt++
	change.UntilMS = min(now+60000, change.Execution.DeadlineAtMS)
	return change, nil
}

func validLeaseCandidate(before LeaseSnapshot, now int64) bool {
	return before.JobState == "QUEUED" && before.AvailableAtMS <= now && before.JobVersion > 0 &&
		before.JobVersion < math.MaxInt64 && before.ImportVersion > 0 && before.ImportVersion < math.MaxInt64 &&
		before.ExecutionNo > 0 && before.Attempt >= 0 && before.Attempt < before.MaxAttempts &&
		now <= math.MaxInt64-(8*time.Hour).Milliseconds()
}

func (service *Leases) Renew(ctx context.Context, unit Execution) (LeaseState, error) {
	state := LeaseLost
	err := service.repository.WithLease(ctx, func(scope LeaseScope) error {
		before, found, err := scope.Read.Current(ctx, unit.JobID)
		if err != nil {
			return fmt.Errorf("read EmulationStation lease: %w", err)
		}
		if !found {
			return nil
		}
		now := service.now().UnixMilli()
		state = ExecutionState(before, unit, now)
		if state != LeaseActive {
			return nil
		}
		if now > math.MaxInt64-60000 {
			return ErrInvalid
		}
		return scope.Write.Renew(ctx, RenewLease{Before: before, NowMS: now, UntilMS: min(now+60000, before.DeadlineAtMS)})
	})
	if err != nil {
		return LeaseLost, fmt.Errorf("renew EmulationStation execution: %w", err)
	}
	return state, nil
}

// ExecutionState validates the current transaction snapshot against the claimed attempt.
func ExecutionState(before LeaseSnapshot, unit Execution, now int64) LeaseState {
	if before.Execution != unit || unit.WorkerID == "" || unit.ExecutionNo <= 0 || unit.Attempt <= 0 ||
		before.JobVersion <= 0 || before.JobVersion == math.MaxInt64 || before.ImportVersion <= 0 ||
		before.ImportVersion == math.MaxInt64 {
		return LeaseLost
	}
	if before.JobState == "CANCEL_REQUESTED" && before.ImportState == "CANCEL_REQUESTED" {
		return LeaseCancelled
	}
	if before.JobState != "RUNNING" || !runningImportState(before) {
		return LeaseLost
	}
	if before.DeadlineAtMS <= now {
		return LeaseDeadline
	}
	if before.LeaseUntilMS <= now {
		return LeaseLost
	}
	return LeaseActive
}

func runningImportState(before LeaseSnapshot) bool {
	return (before.Kind == "SERVER_EMULATIONSTATION_SCAN" && before.ImportState == "SCANNING") ||
		(before.Kind == "SERVER_EMULATIONSTATION_IMPORT" && before.ImportState == "RUNNING")
}
