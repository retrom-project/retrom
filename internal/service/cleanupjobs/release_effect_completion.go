package cleanupjobs

import (
	"context"
	"fmt"
)

func (run *effectRun) finish(ctx context.Context, before EffectOwner) error {
	remains, err := run.scope.Read.Remaining(ctx, before.Owner.Scope)
	if err != nil {
		return fmt.Errorf("read remaining payload references: %w", err)
	}
	if remains != 0 {
		if remains >= run.initialReferences {
			return effectFailure("OWNER_CLEANUP_REFERENCE_REMAINS", nil)
		}
		run.more = true
		return nil
	}
	if run.more {
		return nil
	}
	after := before.Owner
	after.PayloadState = "RELEASED"
	plan, err := ownerCleanupPlan(after.Scope.Type)
	if err != nil {
		return err
	}
	if plan.AdvanceVersion {
		after.Version++
	}
	if err := run.scope.Write.ChangeOwner(
		ctx,
		EffectOwnerChange{Before: before, After: after, Released: true, NowMS: run.nowMS},
	); err != nil {
		return fmt.Errorf("complete payload owner: %w", err)
	}
	before.Owner = after
	run.completed = append(run.completed, before)
	return nil
}

func (run *effectRun) remove(ctx context.Context, before EffectOwner, groups ...EffectReferenceGroup) error {
	for _, group := range groups {
		if err := run.scope.Write.Remove(ctx, EffectRemoval{Before: before, Group: group, NowMS: run.nowMS}); err != nil {
			return fmt.Errorf("remove %s references: %w", group, err)
		}
	}
	return nil
}
