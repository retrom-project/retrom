package pegasusimport

import (
	"context"
	"fmt"
	"math"
	"time"

	model "retrom/internal/model/pegasusimport"

	"github.com/google/uuid"
)

type Leases struct {
	repository model.LeaseRepository
	now        func() time.Time
}

func NewLeases(repository model.LeaseRepository, now func() time.Time) *Leases {
	return &Leases{repository: repository, now: now}
}

func (service *Leases) Claim(ctx context.Context) (model.Work, bool, error) {
	var unit model.Work
	found := false
	err := service.repository.WithLease(ctx, func(records model.LeaseRecords) error {
		now := service.now().UnixMilli()
		before, exists, err := records.Next(ctx, now)
		if err != nil {
			return fmt.Errorf("read Pegasus lease candidate: %w", err)
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
			return fmt.Errorf("generate Pegasus worker identity: %w", err)
		}
		change.Work.WorkerID = id.String()
		if err := records.Claim(ctx, change); err != nil {
			return fmt.Errorf("persist Pegasus claim: %w", err)
		}
		unit, found = change.Work, true
		return nil
	})
	if err != nil {
		return model.Work{}, false, fmt.Errorf("claim Pegasus execution: %w", err)
	}
	return unit, found, nil
}

func planLease(before model.LeaseCandidate, now int64) (model.LeaseClaim, error) {
	if !validLeaseCandidate(before, now) {
		return model.LeaseClaim{}, model.ErrInvalid
	}
	change := model.LeaseClaim{
		Before:      before,
		Work:        before.Work,
		ImportState: "SCANNING",
		Phase:       "DISCOVERING_METADATA",
		NowMS:       now,
		StartedAtMS: now,
	}
	duration := (30 * time.Minute).Milliseconds()
	switch before.Work.Kind {
	case "SERVER_PEGASUS_SCAN":
		if before.ImportState != "SCANNING" {
			return model.LeaseClaim{}, model.ErrVersionConflict
		}
	case "SERVER_PEGASUS_IMPORT":
		if before.ImportState != "QUEUED" {
			return model.LeaseClaim{}, model.ErrVersionConflict
		}
		change.ImportState, change.Phase, duration = "RUNNING", "COPYING_CONTENT", (8 * time.Hour).Milliseconds()
	default:
		return model.LeaseClaim{}, model.ErrInvalid
	}
	if (before.StartedAtMS == nil) != (before.DeadlineAtMS == nil) {
		return model.LeaseClaim{}, model.ErrInvalid
	}
	change.Work.DeadlineAtMS = now + duration
	if before.DeadlineAtMS != nil {
		if *before.DeadlineAtMS <= now {
			return model.LeaseClaim{}, model.ErrExpired
		}
		change.StartedAtMS, change.Work.DeadlineAtMS = *before.StartedAtMS, *before.DeadlineAtMS
	}
	change.Work.Attempt++
	change.LeaseUntilMS = min(now+60000, change.Work.DeadlineAtMS)
	return change, nil
}

func (service *Leases) Renew(ctx context.Context, identity model.ExecutionIdentity) error {
	err := service.repository.WithLease(ctx, func(records model.LeaseRecords) error {
		before, err := records.Current(ctx, identity.JobID)
		if err != nil {
			return fmt.Errorf("read Pegasus lease: %w", err)
		}
		now := service.now().UnixMilli()
		if err := ValidateExecution(before, identity, now); err != nil {
			return err
		}
		if now > math.MaxInt64-60000 {
			return model.ErrInvalid
		}
		return records.Renew(
			ctx,
			model.LeaseRenewal{Before: before, NowMS: now, LeaseUntilMS: min(now+60000, before.DeadlineMS)},
		)
	})
	if err != nil {
		return fmt.Errorf("renew Pegasus execution: %w", err)
	}
	return nil
}

// ValidateExecution checks the current transaction snapshot, not the version at initial claim.
func ValidateExecution(before model.ExecutionSnapshot, identity model.ExecutionIdentity, now int64) error {
	actual := model.ExecutionIdentity{
		JobID: before.JobID, ImportID: before.ImportID, WorkerID: before.WorkerID,
		ExecutionNo: before.ExecutionNo, Attempt: before.Attempt,
	}
	if actual != identity || !validLiveExecution(before, now) {
		return model.ErrVersionConflict
	}
	if before.JobState == "CANCEL_REQUESTED" && before.ImportState == "CANCEL_REQUESTED" {
		return nil
	}
	if before.JobState == "RUNNING" {
		if before.Kind == "SERVER_PEGASUS_SCAN" && before.ImportState == "SCANNING" {
			return nil
		}
		if before.Kind == "SERVER_PEGASUS_IMPORT" && before.ImportState == "RUNNING" {
			return nil
		}
	}
	return model.ErrVersionConflict
}

func validLeaseCandidate(before model.LeaseCandidate, now int64) bool {
	return before.JobVersion > 0 && before.JobVersion < math.MaxInt64 && before.ImportVersion > 0 &&
		before.ImportVersion < math.MaxInt64 && before.Work.ExecutionNo > 0 && before.Work.Attempt >= 0 &&
		before.Work.Attempt < before.MaxAttempts && now <= math.MaxInt64-(8*time.Hour).Milliseconds()
}

func validLiveExecution(before model.ExecutionSnapshot, now int64) bool {
	return before.WorkerID != "" && before.ExecutionNo > 0 && before.Attempt > 0 &&
		before.JobVersion > 0 && before.JobVersion < math.MaxInt64 && before.ImportVersion > 0 &&
		before.ImportVersion < math.MaxInt64-1 && before.LeaseUntilMS > now && before.DeadlineMS > now
}
