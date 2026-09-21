package pegasusimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/pegasusimport"
)

const materialItemFence = ` AND EXISTS(SELECT 1 FROM pegasus_import_items item
WHERE item.id=? AND item.import_id=? AND item.version=? AND item.execution_state=?
AND item.execution_state='COPYING'` + itemExecutionFence + `)`

func (records materialRecords) Bind(ctx context.Context, change application.MaterialBinding) (string, error) {
	source := change.Before.Source
	mediaType := source.MediaType
	if source.Key.Kind == "" {
		mediaType = "application/octet-stream"
	}
	blobID, err := registerVerifiedMaterial(ctx, records.tx, change.Blob, mediaType, change.NowMS)
	if err != nil {
		return "", err
	}
	update := recordstore.Update{
		Set:    `blob_id=?,state='COPIED',updated_at_ms=?`,
		Values: []any{blobID, change.NowMS},
		Scope:  materialScope(change.Before, change.NowMS),
	}
	if source.Key.Kind == "" {
		result, err := recordstore.UpdatePegasusImportItemFiles(ctx, records.tx, update)
		if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
			return "", err
		}
	} else {
		result, err := recordstore.UpdatePegasusImportItemAssets(ctx, records.tx, update)
		if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
			return "", err
		}
	}
	return blobID, nil
}

func materialScope(before application.MaterialSnapshot, now int64) recordstore.Scope {
	source := before.Source
	where := `item_id=? AND ordinal=?`
	key := any(source.Key.Ordinal)
	if source.Key.Kind != "" {
		where = `item_id=? AND kind=?`
		key = source.Key.Kind
	}
	args := []any{
		source.Key.ItemID,
		key,
		source.Path,
		source.Size,
		source.Facts,
		before.State,
		optionalText(before.BlobID),
	}
	where += ` AND relative_path=? AND size_bytes=? AND source_facts_digest=?
AND state=? AND blob_id IS ? AND state='DISCOVERED'`
	if source.Key.Kind != "" {
		where += ` AND COALESCE(media_type,'')=? AND width_px IS ? AND height_px IS ?`
		args = append(args, source.MediaType, source.Width, source.Height)
	}
	where += materialItemFence
	args = append(args, itemFenceArgs(before.Before, now)...)
	return recordstore.Scope{Where: where, Args: args}
}

func (records materialRecords) Warn(ctx context.Context, change application.MaterialWarning) error {
	warnings, err := json.Marshal(change.Warnings)
	if err != nil {
		return fmt.Errorf("encode Pegasus asset warnings: %w", err)
	}
	result, err := recordstore.UpdatePegasusImportItemAssets(ctx, records.tx, recordstore.Update{
		Set: `state=?,warning_code=?,updated_at_ms=?`, Values: []any{
			change.State,
			change.Code,
			change.NowMS,
		}, Scope: materialScope(
			change.Before,
			change.NowMS,
		),
	})
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	result, err = recordstore.UpdatePegasusImportItems(ctx, records.tx, recordstore.Update{
		Set: `warnings_json=?,version=version+1,updated_at_ms=?`, Values: []any{string(warnings), change.NowMS},
		Scope: recordstore.Scope{
			Where: `id=? AND import_id=? AND version=? AND execution_state=?` + itemExecutionFence,
			Args:  itemFenceArgs(change.Before.Before, change.NowMS),
		},
	})
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}

func (records materialRecords) Phase(ctx context.Context, change application.PhaseChange) error {
	before := change.Before.Execution
	// The execution fence shares the exact parent pointer and live lease checks used by item work.
	owned := application.OwnedItem{Execution: before}
	args := make([]any, 0, 18)
	args = append(args, before.ImportID, before.ImportVersion, before.ImportState, change.Before.Phase)
	args = append(args, itemFenceArgs(owned, change.NowMS)[4:]...)
	result, err := recordstore.UpdatePegasusImports(ctx, records.tx, recordstore.Update{
		Set: `phase=?,version=version+1,updated_at_ms=?`, Values: []any{change.Phase, change.NowMS},
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND state=? AND COALESCE(phase,'')=? AND state='RUNNING'` + itemExecutionFence,
			Args:  args,
		},
	})
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}
