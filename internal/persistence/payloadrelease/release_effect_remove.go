package payloadrelease

import (
	"context"

	gamerelease "retrom/internal/persistence/gamecontent/gamerelease"
	itemrelease "retrom/internal/persistence/libraryimport/itemrelease"
	"retrom/internal/persistence/releaseops"
	sourcerelease "retrom/internal/persistence/sourceimport/sourcerelease"
	application "retrom/internal/service/payloadrelease"
)

func (records effectRecords) Remove(ctx context.Context, change application.EffectRemoval) error {
	if !validEffectRemoval(change.Group, change.Before.Owner.Scope.Type) {
		return application.ErrScopeInvalid
	}
	if err := records.fenceOwner(ctx, change.Before); err != nil {
		return err
	}
	id, now := change.Before.Owner.Scope.ID, change.NowMS
	switch change.Group {
	case application.EffectGameRuntime:
		return (gamerelease.Records{Executor: records.executor}).StopRuntime(ctx, id, now)
	case application.EffectGameEvidence:
		return (gamerelease.Records{Executor: records.executor}).ClearEvidence(ctx, id, now)
	case application.EffectGameFiles:
		return records.removeBatches(ctx, gamerelease.DeleteStatements(), id)
	case application.EffectImportReview:
		return (itemrelease.Records{Executor: records.executor}).ClearReview(ctx, id, now)
	case application.EffectImportEvidence:
		return (itemrelease.Records{Executor: records.executor}).ClearEvidence(ctx, id, now)
	case application.EffectImportFiles:
		return records.removeBatches(ctx, itemrelease.DeleteStatements(), id)
	case application.EffectSourceFiles, application.EffectSourceAssets:
		return sourcerelease.Clear(ctx, records.executor, change)
	default:
		return application.ErrScopeInvalid
	}
}

func (records effectRecords) removeBatches(ctx context.Context, batches []releaseops.DeletionBatch, id string) error {
	return (releaseops.Records{Executor: records.executor}).RemoveBatches(ctx, batches, id)
}
