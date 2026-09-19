package payloadrelease

import (
	"context"
	"fmt"

	model "retrom/internal/model/payloadrelease"
)

func recheckEffectMutations(ctx context.Context, reader model.EffectReader, scope model.Scope) error {
	if scope.Type != model.ScopeGame {
		return nil
	}
	active, err := reader.Mutations(ctx, scope)
	if err != nil {
		return fmt.Errorf("recheck game mutations: %w", err)
	}
	if active != 0 {
		return effectFailure("PAYLOAD_RELEASE_DEPENDENCY_PENDING", nil)
	}
	return nil
}

func (run *effectRun) execute(ctx context.Context, before model.EffectOwner, reason model.Reason) error {
	if before.Owner.Scope.Type == model.ScopeUploadConsumption {
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
			return model.ErrEffectConflict
		}
	}
	return nil
}
