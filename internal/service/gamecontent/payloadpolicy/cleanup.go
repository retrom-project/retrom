package payloadpolicy

import (
	"context"
	"fmt"
	"time"

	jobs "retrom/internal/service/cleanupjobs"
	uploadcleanup "retrom/internal/service/uploads/payloadpolicy"
)

func Cleanup(repository jobs.EffectRepository, waiter jobs.EffectWaiter) jobs.EffectBinding {
	prepare := func(ctx context.Context, scope jobs.Scope) error {
		for {
			active, err := repository.ActiveMutations(ctx, scope)
			if err != nil {
				return fmt.Errorf("read game mutations: %w", err)
			}
			if active == 0 {
				return nil
			}
			if err := waiter.Wait(ctx, 100*time.Millisecond); err != nil {
				return fmt.Errorf("wait for game mutations: %w", err)
			}
		}
	}
	return jobs.EffectBinding{Repository: repository, Apply: Release, Prepare: prepare}
}

func Release(ctx context.Context, scope jobs.EffectScope, unit jobs.Execution, now int64) (bool, error) {
	before, err := scope.Read.Owner(ctx, unit.Work.Scope)
	if err != nil {
		return false, fmt.Errorf("read game cleanup owner: %w", err)
	}
	if !ReleaseReady(before.Owner.State) {
		return false, jobs.ErrOwnerNotTerminal
	}
	if err := jobs.CheckReleaseOwner(unit, before, false); err != nil {
		return false, fmt.Errorf("release gamecontent payload: %w", err)
	}
	if before.Owner.PayloadState == "RELEASED" {
		return false, nil
	}
	active, err := scope.Read.Mutations(ctx, unit.Work.Scope)
	if err != nil {
		return false, fmt.Errorf("recheck game mutations: %w", err)
	}
	if active != 0 {
		return false, jobs.ErrDependencyPending
	}
	initial, err := scope.Read.Remaining(ctx, unit.Work.Scope)
	if err != nil {
		return false, fmt.Errorf("read game payload: %w", err)
	}
	payload, err := scope.Read.Payload(ctx, unit.Work.Scope)
	if err != nil {
		return false, fmt.Errorf("read game consumptions: %w", err)
	}
	if err := uploadcleanup.ConsumePayload(ctx, scope, payload, jobs.ReasonGameDeleted, now); err != nil {
		return false, fmt.Errorf("release gamecontent payload: %w", err)
	}
	if err := scope.Write.Clear(ctx, before, now); err != nil {
		return false, fmt.Errorf("clear game payload: %w", err)
	}
	more, err := uploadcleanup.PurgePayload(ctx, scope, payload, now)
	if err != nil {
		return false, fmt.Errorf("release gamecontent payload: %w", err)
	}
	value, err := jobs.FinishRelease(ctx, scope, before, initial, more, true, now)
	if err != nil {
		return false, fmt.Errorf("complete gamecontent cleanup: %w", err)
	}
	return value, nil
}
