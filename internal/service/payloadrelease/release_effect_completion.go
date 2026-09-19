package payloadrelease

import (
	"context"
	"fmt"

	model "retrom/internal/model/payloadrelease"
)

func (run *effectRun) retry(ctx context.Context, before model.EffectOwner) (model.EffectOwner, error) {
	if before.Owner.PayloadState != "FAILED" {
		return before, nil
	}
	after := before.Owner
	after.PayloadState = "RELEASING"
	if after.Scope.Type == model.ScopeGame {
		after.Version++
	}
	change := model.EffectOwnerChange{Before: before, After: after, NowMS: run.nowMS}
	if err := run.scope.Write.ChangeOwner(ctx, change); err != nil {
		return model.EffectOwner{}, fmt.Errorf("retry payload owner: %w", err)
	}
	before.Owner = after
	return before, nil
}

func (run *effectRun) finish(ctx context.Context, before model.EffectOwner) error {
	remains, err := run.scope.Read.Remaining(ctx, before.Owner.Scope)
	if err != nil {
		return fmt.Errorf("read remaining payload references: %w", err)
	}
	if remains != 0 {
		return effectFailure("PAYLOAD_RELEASE_REFERENCE_REMAINS", nil)
	}
	after := before.Owner
	after.PayloadState = "RELEASED"
	if after.Scope.Type == model.ScopeGame ||
		after.Scope.Type == model.ScopePegasusImportItem || after.Scope.Type == model.ScopeEmulationStationImportItem {
		after.Version++
	}
	if err := run.scope.Write.ChangeOwner(
		ctx, model.EffectOwnerChange{Before: before, After: after, Released: true, NowMS: run.nowMS},
	); err != nil {
		return fmt.Errorf("complete payload owner: %w", err)
	}
	before.Owner = after
	run.completed = append(run.completed, before)
	return nil
}

func (run *effectRun) remove(
	ctx context.Context,
	before model.EffectOwner,
	groups ...model.EffectReferenceGroup,
) error {
	for _, group := range groups {
		if err := run.scope.Write.Remove(ctx, model.EffectRemoval{
			Before: before,
			Group:  group,
			NowMS:  run.nowMS,
		}); err != nil {
			return fmt.Errorf("remove %s references: %w", group, err)
		}
	}
	return nil
}
