package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/dbexec"
	"retrom/internal/persistence/recordstore"
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
		return records.stopGameRuntime(ctx, id, now)
	case application.EffectGameEvidence:
		return records.clearGameEvidence(ctx, id, now)
	case application.EffectGameFiles:
		return records.removeBatches(ctx, gameEffectDeleteStatements(), id)
	case application.EffectImportReview:
		return records.clearImportReview(ctx, id, now)
	case application.EffectImportEvidence:
		return records.clearImportEvidence(ctx, id, now)
	case application.EffectImportFiles:
		return records.removeBatches(ctx, importEffectDeleteStatements(), id)
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
	err = records.checkedUpdate(ctx, table, update, query)
	return err
}

func (records effectRecords) checkedUpdate(
	ctx context.Context,
	table string,
	write effectRecordUpdate,
	update recordstore.Update,
) error {
	count, err := records.readCount(ctx, "SELECT count(*) FROM "+table+" WHERE "+update.Scope.Where, update.Scope.Args...)
	if err != nil {
		return err
	}
	result, err := write(ctx, records.executor, update)
	if err := effectCount(result, err, count); err != nil {
		return err
	}
	return nil
}

func (records effectRecords) execUpdate(
	ctx context.Context,
	table, where string,
	countArgs []any,
	query string,
	args ...any,
) error {
	count, err := records.readCount(ctx, "SELECT count(*) FROM "+table+" WHERE "+where, countArgs...)
	if err != nil {
		return err
	}
	result, err := records.executor.ExecContext(ctx, query, args...)
	if err := effectCount(result, err, count); err != nil {
		return err
	}
	return nil
}

type effectDeletionBatch struct {
	table, where string
	remove       func(context.Context, dbexec.Executor, recordstore.Scope) (sql.Result, error)
}

func (records effectRecords) removeBatches(ctx context.Context, batches []effectDeletionBatch, id string) error {
	for _, batch := range batches {
		if err := records.removeBatch(ctx, batch, id); err != nil {
			return err
		}
	}
	return nil
}

func (records effectRecords) removeBatch(ctx context.Context, batch effectDeletionBatch, id string) error {
	for {
		count, err := records.readCount(ctx, "SELECT count(*) FROM "+batch.table+" WHERE "+batch.where, id)
		if err != nil {
			return err
		}
		result, err := batch.remove(ctx, records.executor, recordstore.Scope{Where: batch.where, Args: []any{id}})
		if err := effectCount(result, err, count); err != nil {
			return fmt.Errorf("delete payload reference batch: %w", err)
		}
		if count < 200 {
			return nil
		}
	}
}
