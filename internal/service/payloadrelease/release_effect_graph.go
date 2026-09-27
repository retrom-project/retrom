package payloadrelease

import (
	"context"
	"fmt"
)

func (run *effectRun) game(ctx context.Context, before EffectOwner) error {
	payload, err := run.scope.Read.Payload(ctx, before.Owner.Scope)
	if err != nil {
		return fmt.Errorf("read game payload: %w", err)
	}
	if err := run.consumePayload(ctx, payload, effectReason(before.Owner)); err != nil {
		return err
	}
	if err := run.remove(ctx, before, EffectGameRuntime, EffectGameEvidence, EffectGameFiles); err != nil {
		return err
	}
	if err := run.purgePayload(ctx, payload); err != nil {
		return err
	}
	return run.finish(ctx, before)
}

func (run *effectRun) item(ctx context.Context, before EffectOwner) error {
	payload, err := run.scope.Read.Payload(ctx, before.Owner.Scope)
	if err != nil {
		return fmt.Errorf("read ordinary item payload: %w", err)
	}
	if err := run.consumePayload(ctx, payload, effectReason(before.Owner)); err != nil {
		return err
	}
	if err := run.remove(ctx, before, EffectImportReview, EffectImportFiles, EffectImportEvidence); err != nil {
		return err
	}
	if err := run.purgePayload(ctx, payload); err != nil {
		return err
	}
	return run.finish(ctx, before)
}

func (run *effectRun) aggregate(ctx context.Context, before EffectOwner) error {
	links, err := run.scope.Read.Links(ctx, before.Owner.Scope)
	if err != nil {
		return fmt.Errorf("read aggregate release children: %w", err)
	}
	for _, link := range links {
		child, err := run.scope.Read.Owner(ctx, link)
		if err != nil {
			return fmt.Errorf("read aggregate child: %w", err)
		}
		if !validEffectChild(before, child) {
			return effectFailure("PAYLOAD_RELEASE_DEPENDENCY_PENDING", nil)
		}
	}
	payload, err := run.scope.Read.Payload(ctx, before.Owner.Scope)
	if err != nil {
		return fmt.Errorf("read aggregate payload: %w", err)
	}
	if err := run.consumePayload(ctx, payload, ReasonImportTerminal); err != nil {
		return err
	}
	if err := run.purgePayload(ctx, payload); err != nil {
		return err
	}
	return run.finish(ctx, before)
}

func (run *effectRun) source(ctx context.Context, before EffectOwner) error {
	if err := run.remove(ctx, before, EffectSourceFiles, EffectSourceAssets); err != nil {
		return err
	}
	return run.finish(ctx, before)
}
