package payloadpolicy

import (
	"context"
	"fmt"

	jobs "retrom/internal/service/cleanupjobs"
)

func Release(ctx context.Context, scope jobs.EffectScope, unit jobs.Execution, now int64) (bool, error) {
	before, err := scope.Read.Owner(ctx, unit.Work.Scope)
	if err != nil {
		return false, fmt.Errorf("read source cleanup owner: %w", err)
	}
	owner := before.Owner
	if !ReleaseReady(owner.State, owner.Retryable, owner.PublicID) {
		return false, jobs.ErrOwnerNotTerminal
	}
	if err := jobs.CheckReleaseOwner(unit, before, true); err != nil {
		return false, fmt.Errorf("release sourceimport payload: %w", err)
	}
	if owner.PayloadState == "RELEASED" {
		return false, nil
	}
	initial, err := scope.Read.Remaining(ctx, unit.Work.Scope)
	if err != nil {
		return false, fmt.Errorf("read source payload: %w", err)
	}
	if err := scope.Write.Clear(ctx, before, now); err != nil {
		return false, fmt.Errorf("clear source payload: %w", err)
	}
	value, err := jobs.FinishRelease(ctx, scope, before, initial, false, true, now)
	if err != nil {
		return false, fmt.Errorf("complete sourceimport cleanup: %w", err)
	}
	return value, nil
}
