package cleanupjobs

import (
	"context"
	"fmt"
)

// FinishRelease commits bounded progress and fences the resulting owner state.
func FinishRelease(ctx context.Context, scope EffectScope, before EffectOwner,
	initial int64, more, advance bool, now int64,
) (bool, error) {
	remaining, err := scope.Read.Remaining(ctx, before.Owner.Scope)
	if err != nil {
		return false, fmt.Errorf("read remaining payload references: %w", err)
	}
	if remaining != 0 {
		if remaining >= initial {
			return false, Failure("OWNER_CLEANUP_REFERENCE_REMAINS", nil)
		}
		return true, nil
	}
	if more {
		return true, nil
	}
	after := before.Owner
	after.PayloadState = "RELEASED"
	if advance {
		after.Version++
	}
	if err := scope.Write.ChangeOwner(ctx, EffectOwnerChange{
		Before: before, After: after, Released: true, NowMS: now,
	}); err != nil {
		return false, fmt.Errorf("complete payload owner: %w", err)
	}
	current, err := scope.Read.Owner(ctx, before.Owner.Scope)
	if err != nil {
		return false, fmt.Errorf("confirm payload owner: %w", err)
	}
	before.Owner = after
	if current != before {
		return false, ErrEffectConflict
	}
	return false, nil
}
