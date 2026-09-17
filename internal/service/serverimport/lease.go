package serverimport

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	model "retrom/internal/model/serverimport"
)

type Leases struct {
	repository model.LeaseRepository
	now        func() time.Time
}

func NewLeases(
	repository model.LeaseRepository, now func() time.Time,
) *Leases {
	return &Leases{repository, now}
}

func (service *Leases) Claim(
	ctx context.Context,
) (model.Work, bool, error) {
	result, err := service.repository.CommitClaim(
		ctx, model.ClaimCommand{Now: service.now().UnixMilli()},
	)
	if err != nil {
		return model.Work{}, false,
			fmt.Errorf("claim server import: %w", err)
	}
	return result.Unit, result.Found, nil
}

func (service *Leases) Heartbeat(
	ctx context.Context, unit model.Work,
) error {
	return service.touch(ctx, unit, "", nil)
}

func (service *Leases) Progress(
	ctx context.Context,
	unit model.Work,
	phase string,
	current, total int64,
) error {
	event, err := json.Marshal(map[string]any{
		"schemaVersion": 1,
		"phase":         phase,
		"completed":     current,
		"total":         total,
	})
	if err != nil {
		return fmt.Errorf("encode import progress: %w", err)
	}
	return service.touch(ctx, unit, phase, event)
}

func (service *Leases) touch(
	ctx context.Context, unit model.Work, phase string, event []byte,
) error {
	err := service.repository.CommitTouch(ctx, model.TouchCommand{
		Unit: unit, Phase: phase, Event: event,
		Now: service.now().UnixMilli(),
	})
	if err != nil {
		return fmt.Errorf("update import progress: %w", err)
	}
	return nil
}
