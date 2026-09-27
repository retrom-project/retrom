package cleanupjobs

import (
	"context"
	"fmt"
)

func (service *ReleaseEffects) Execute(ctx context.Context, unit Execution) error {
	frozen, err := DecodeWork(unit.Work)
	if err != nil {
		return err
	}
	if frozen != unit.Input || unit.Work.Kind != "OWNER_CLEANUP" {
		return ErrInputInvalid
	}
	if service.binding.Prepare != nil {
		if err := service.binding.Prepare(ctx, unit.Work.Scope); err != nil {
			return err
		}
	}
	for {
		more, err := service.step(ctx, unit)
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
	}
}

func (service *ReleaseEffects) step(ctx context.Context, unit Execution) (bool, error) {
	var more bool
	err := service.binding.Repository.WithEffects(ctx, func(scope EffectScope) error {
		if err := service.authority.CheckInScope(ctx, scope.Worker, unit.Work); err != nil {
			return fmt.Errorf("check cleanup authority: %w", err)
		}
		var err error
		more, err = service.binding.Apply(ctx, scope, unit, service.now().UnixMilli())
		if err != nil {
			return err
		}
		if err := service.authority.CheckInScope(ctx, scope.Worker, unit.Work); err != nil {
			return fmt.Errorf("confirm cleanup authority: %w", err)
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("commit owner cleanup: %w", err)
	}
	return more, nil
}
