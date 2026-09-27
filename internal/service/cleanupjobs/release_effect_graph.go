package cleanupjobs

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
)

func (run *effectRun) apply(ctx context.Context, before EffectOwner, plan cleanup.Plan) error {
	if plan.RequireChildrenReleased {
		if err := run.requireChildren(ctx, before); err != nil {
			return err
		}
	}
	var payload EffectPayload
	if plan.ConsumeUploads {
		var err error
		payload, err = run.scope.Read.Payload(ctx, before.Owner.Scope)
		if err != nil {
			return fmt.Errorf("read cleanup uploads: %w", err)
		}
		if err := run.consumePayload(ctx, payload, effectReason(before.Owner)); err != nil {
			return err
		}
	}
	for _, group := range plan.Groups {
		if err := run.remove(ctx, before, EffectReferenceGroup(group)); err != nil {
			return err
		}
	}
	if plan.ConsumeUploads {
		if err := run.purgePayload(ctx, payload); err != nil {
			return err
		}
	}
	return run.finish(ctx, before)
}

func (run *effectRun) requireChildren(ctx context.Context, before EffectOwner) error {
	links, err := run.scope.Read.Links(ctx, before.Owner.Scope)
	if err != nil {
		return fmt.Errorf("read cleanup children: %w", err)
	}
	for _, link := range links {
		child, err := run.scope.Read.Owner(ctx, link)
		if err != nil {
			return fmt.Errorf("read cleanup child: %w", err)
		}
		if !validEffectChild(before, child) {
			return effectFailure("OWNER_CLEANUP_DEPENDENCY_PENDING", nil)
		}
	}
	return nil
}
