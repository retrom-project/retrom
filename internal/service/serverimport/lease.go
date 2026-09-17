package serverimport

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	model "retrom/internal/model/serverimport"

	"github.com/google/uuid"
)

type Leases struct {
	repository model.LeaseRepository
	now        func() time.Time
}

func NewLeases(repository model.LeaseRepository, now func() time.Time) *Leases {
	return &Leases{repository, now}
}

func (service *Leases) Claim(ctx context.Context) (model.Work, bool, error) {
	var result model.Work
	var found bool
	err := service.repository.CommitWrite(ctx, func(records model.LeaseRecords) error {
		now := service.now().UnixMilli()
		before, ok, err := records.Next(ctx, now)
		if err != nil {
			return fmt.Errorf("read claimable import: %w", err)
		}
		if !ok {
			return nil
		}
		if before.State != before.ImportState || before.Attempt >= before.Maximum {
			return model.ErrLeaseLost
		}
		owner, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("create import worker identity: %w", err)
		}
		unit := before.Work
		unit.Owner = owner.String()
		unit.DeadlineAtMS = now + (8 * time.Hour).Milliseconds()
		if before.Deadline != nil {
			unit.DeadlineAtMS = *before.Deadline
		}
		plan := model.LeaseClaim{
			Before:     before,
			Work:       unit,
			Now:        now,
			LeaseUntil: now + time.Minute.Milliseconds(),
			Event: []byte(
				`{"schemaVersion":1,"phase":"PREPARING_ROOT"}`,
			),
		}
		if before.State == "RUNNING" {
			plan.RecoveryEvent = []byte(`{"schemaVersion":1,"reason":"LEASE_EXPIRED"}`)
		}
		if err := records.Claim(ctx, plan); err != nil {
			return fmt.Errorf("claim import execution: %w", err)
		}
		result, found = unit, true
		return nil
	})
	if err != nil {
		return model.Work{}, false, fmt.Errorf("claim server import: %w", err)
	}
	return result, found, nil
}

func (service *Leases) Heartbeat(ctx context.Context, unit model.Work) error {
	return service.touch(ctx, unit, "", nil)
}

func (service *Leases) Progress(ctx context.Context, unit model.Work, phase string, current, total int64) error {
	event, err := json.Marshal(map[string]any{"schemaVersion": 1, "phase": phase, "completed": current, "total": total})
	if err != nil {
		return fmt.Errorf("encode import progress: %w", err)
	}
	return service.touch(ctx, unit, phase, event)
}

func (service *Leases) touch(ctx context.Context, unit model.Work, phase string, event []byte) error {
	err := service.repository.CommitWrite(ctx, func(records model.LeaseRecords) error {
		before, err := records.Current(ctx, unit.JobID)
		if err != nil {
			return fmt.Errorf("read import lease: %w", err)
		}
		now := service.now().UnixMilli()
		if before.Work.ImportID != unit.ImportID || before.Work.Execution != unit.Execution ||
			before.Work.Owner != unit.Owner || unit.Owner == "" {
			return model.ErrLeaseLost
		}
		if before.State == "CANCEL_REQUESTED" || before.State == "CANCELLED" {
			return model.ErrWorkerCancelled
		}
		if before.State != "RUNNING" || before.ImportState != "RUNNING" ||
			before.LeaseUntil == nil || *before.LeaseUntil <= now {
			return model.ErrLeaseLost
		}
		if err := records.Touch(
			ctx,
			model.LeaseTouch{
				Before:     before,
				Now:        now,
				LeaseUntil: now + time.Minute.Milliseconds(),
				Phase:      phase,
				Event:      event,
			},
		); err != nil {
			return fmt.Errorf("renew import lease: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("update import progress: %w", err)
	}
	return nil
}
