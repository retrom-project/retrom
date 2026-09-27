package cleanupjobs

import (
	"context"
	"fmt"
)

func recheckEffectMutations(ctx context.Context, reader EffectReader, scope Scope) error {
	plan, _ := ownerCleanupPlan(scope.Type)
	if !plan.WaitForMutations {
		return nil
	}
	active, err := reader.Mutations(ctx, scope)
	if err != nil {
		return fmt.Errorf("recheck game mutations: %w", err)
	}
	if active != 0 {
		return effectFailure("OWNER_CLEANUP_DEPENDENCY_PENDING", nil)
	}
	return nil
}

func (run *effectRun) execute(ctx context.Context, before EffectOwner, reason Reason) error {
	if before.Owner.Scope.Type == ScopeUploadConsumption {
		return run.consumption(ctx, before, reason)
	}
	return run.release(ctx, before)
}

func (run *effectRun) confirm(ctx context.Context) error {
	for _, before := range run.completed {
		current, err := run.scope.Read.Owner(ctx, before.Owner.Scope)
		if err != nil {
			return fmt.Errorf("confirm payload owner: %w", err)
		}
		if current != before {
			return ErrEffectConflict
		}
	}
	return nil
}
