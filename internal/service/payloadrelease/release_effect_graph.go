package payloadrelease

import (
	"context"
	"fmt"

	model "retrom/internal/model/payloadrelease"
)

func (run *effectRun) game(ctx context.Context, before model.EffectOwner) error {
	payload, err := run.scope.Read.Payload(ctx, before.Owner.Scope)
	if err != nil {
		return fmt.Errorf("read game payload: %w", err)
	}
	links := gameEffectSources(before)
	for _, link := range links {
		if err := run.gameSource(ctx, link); err != nil {
			return err
		}
	}
	if err := run.consumePayload(ctx, payload, effectReason(before.Owner)); err != nil {
		return err
	}
	if err := run.remove(
		ctx,
		before,
		model.EffectGameRuntime,
		model.EffectGameEvidence,
		model.EffectGameFiles,
	); err != nil {
		return err
	}
	if err := run.purgePayload(ctx, payload); err != nil {
		return err
	}
	return run.finish(ctx, before)
}

func (run *effectRun) gameSource(ctx context.Context, link model.Scope) error {
	source, err := run.scope.Read.Owner(ctx, link)
	if err != nil {
		return fmt.Errorf("read game source: %w", err)
	}
	if !source.Found {
		return effectFailure("PAYLOAD_RELEASE_SOURCE_NOT_TERMINAL", nil)
	}
	if source.Owner.PublicID != "" {
		return run.gameSource(ctx, model.Scope{Type: model.ScopeImportItem, ID: source.Owner.PublicID})
	}
	if !terminalEffectOwner(source.Owner) || source.Owner.PayloadState == "RETAINED" || source.Owner.ReleaseJobID == "" {
		return effectFailure("PAYLOAD_RELEASE_SOURCE_NOT_TERMINAL", nil)
	}
	return run.release(ctx, source)
}

func (run *effectRun) item(ctx context.Context, before model.EffectOwner) error {
	payload, err := run.scope.Read.Payload(ctx, before.Owner.Scope)
	if err != nil {
		return fmt.Errorf("read ordinary item payload: %w", err)
	}
	if err := run.consumePayload(ctx, payload, effectReason(before.Owner)); err != nil {
		return err
	}
	if err := run.remove(
		ctx,
		before,
		model.EffectImportReview,
		model.EffectImportFiles,
		model.EffectImportEvidence,
	); err != nil {
		return err
	}
	links, err := run.scope.Read.Links(ctx, before.Owner.Scope)
	if err != nil {
		return fmt.Errorf("read bound source items: %w", err)
	}
	for _, link := range links {
		if err := run.boundSource(ctx, before, link); err != nil {
			return err
		}
	}
	if err := run.purgePayload(ctx, payload); err != nil {
		return err
	}
	return run.finish(ctx, before)
}

func (run *effectRun) aggregate(ctx context.Context, before model.EffectOwner) error {
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
		if err := run.release(ctx, child); err != nil {
			return err
		}
	}
	payload, err := run.scope.Read.Payload(ctx, before.Owner.Scope)
	if err != nil {
		return fmt.Errorf("read aggregate payload: %w", err)
	}
	if err := run.consumePayload(ctx, payload, model.ReasonImportTerminal); err != nil {
		return err
	}
	if err := run.purgePayload(ctx, payload); err != nil {
		return err
	}
	return run.finish(ctx, before)
}

func (run *effectRun) boundSource(ctx context.Context, public model.EffectOwner, link model.Scope) error {
	source, err := run.scope.Read.Owner(ctx, link)
	if err != nil {
		return fmt.Errorf("read bound source owner: %w", err)
	}
	if !source.Found || source.Owner.PublicID != public.Owner.Scope.ID || !terminalBoundEffect(source) {
		return effectFailure("PAYLOAD_RELEASE_SOURCE_NOT_TERMINAL", nil)
	}
	if source.Owner.PayloadState == "RETAINED" {
		after := source.Owner
		after.PayloadState = "RELEASING"
		after.ReleaseJobID = public.Owner.ReleaseJobID
		after.Version++
		if err := run.scope.Write.ChangeOwner(
			ctx, model.EffectOwnerChange{Before: source, After: after, NowMS: run.nowMS},
		); err != nil {
			return fmt.Errorf("bind source release job: %w", err)
		}
		source.Owner = after
	}
	if source.Owner.ReleaseJobID != public.Owner.ReleaseJobID {
		return model.ErrEffectConflict
	}
	return run.release(ctx, source)
}

func terminalBoundEffect(source model.EffectOwner) bool {
	return source.Owner.State == "PUBLISHED" || source.Owner.State == "REVIEW_DISCARDED" ||
		source.Owner.State == "SKIPPED_EXISTING" && source.DuplicateMatch
}

func (run *effectRun) source(ctx context.Context, before model.EffectOwner) error {
	payload, err := run.scope.Read.Payload(ctx, before.Owner.Scope)
	if err != nil {
		return fmt.Errorf("read server item payload: %w", err)
	}
	if err := run.remove(ctx, before, model.EffectSourceFiles, model.EffectSourceAssets); err != nil {
		return err
	}
	run.blobs = append(run.blobs, payload.BlobIDs...)
	return run.finish(ctx, before)
}
