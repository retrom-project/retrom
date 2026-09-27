package payloadrelease

import (
	"context"

	gamerelease "retrom/internal/persistence/gamecontent/gamerelease"
	itemrelease "retrom/internal/persistence/libraryimport/itemrelease"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/releaseops"
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
		return records.clearSource(ctx, change)
	default:
		return application.ErrScopeInvalid
	}
}

func (records effectRecords) clearSource(ctx context.Context, change application.EffectRemoval) error {
	spec, err := effectSourceSpec(change.Before.Owner.Scope.Type)
	if err != nil {
		return err
	}
	now := change.NowMS
	query := recordstore.Update{
		Set:    `state='PAYLOAD_RELEASED',blob_id=NULL,payload_released_at_ms=?,updated_at_ms=?`,
		Values: []any{now, now},
		Scope:  recordstore.Scope{Where: "item_id=? AND blob_id IS NOT NULL", Args: []any{change.Before.Owner.Scope.ID}},
	}
	table, update := spec.assetsTable, spec.updateAssets
	if change.Group == application.EffectSourceFiles {
		table, update = spec.filesTable, spec.updateFiles
		query.Set = `state='PAYLOAD_RELEASED',blob_id=NULL,source_archive_blob_id=NULL,source_archive_entry_ordinal=NULL,
payload_released_at_ms=?,updated_at_ms=?`
		query.Scope.Where = "item_id=? AND (blob_id IS NOT NULL OR source_archive_blob_id IS NOT NULL)"
	}
	err = (releaseops.Records{Executor: records.executor}).CheckedUpdate(ctx, table, update, query)
	return err
}

func (records effectRecords) removeBatches(ctx context.Context, batches []releaseops.DeletionBatch, id string) error {
	return (releaseops.Records{Executor: records.executor}).RemoveBatches(ctx, batches, id)
}
