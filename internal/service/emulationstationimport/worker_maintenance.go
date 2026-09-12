package emulationstationimport

import (
	"context"
	"fmt"
)

type (
	WorkRecovery interface{ Recover(context.Context) error }
	WorkExpiry   interface{ Expire(context.Context) error }
	Maintenance  struct {
		recovery WorkRecovery
		expiry   WorkExpiry
	}
)

func NewMaintenance(recovery WorkRecovery, expiry WorkExpiry) *Maintenance {
	return &Maintenance{recovery: recovery, expiry: expiry}
}

func (service *Maintenance) Maintain(ctx context.Context) error {
	if err := service.recovery.Recover(ctx); err != nil {
		return fmt.Errorf("recover EmulationStation work: %w", err)
	}
	if err := service.expiry.Expire(ctx); err != nil {
		return fmt.Errorf("expire EmulationStation plans: %w", err)
	}
	return nil
}
