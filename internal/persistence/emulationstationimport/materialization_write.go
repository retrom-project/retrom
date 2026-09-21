package emulationstationimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/emulationstationimport"
)

func (records materialRecords) Bind(ctx context.Context, change application.MaterialBinding) (string, error) {
	if err := records.fence(ctx, change.Before, change.NowMS); err != nil {
		return "", err
	}
	source := change.Before.Source
	mediaType := source.MediaType
	if source.Key.Kind == "" {
		mediaType = "application/octet-stream"
	}
	blobID, err := registerVerifiedMaterial(ctx, records.executor, change.Blob, mediaType, change.NowMS)
	if err != nil {
		return "", err
	}
	update := recordstore.Update{
		Set:    `blob_id=?,state='COPIED',updated_at_ms=?`,
		Values: []any{blobID, change.NowMS},
		Scope:  materialScope(change.Before),
	}
	if source.Key.Kind == "" {
		result, err := recordstore.UpdateEmulationstationImportItemFiles(ctx, records.executor, update)
		if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
			return "", err
		}
	} else {
		result, err := recordstore.UpdateEmulationstationImportItemAssets(ctx, records.executor, update)
		if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
			return "", err
		}
	}
	return blobID, nil
}

func (records materialRecords) fence(ctx context.Context, before application.MaterialSnapshot, now int64) error {
	if err := executionRecords(records).Fence(ctx, before.Before.Execution, now); err != nil {
		return err
	}
	result, err := records.executor.ExecContext(
		ctx,
		`UPDATE emulationstation_import_items SET version=version WHERE `+ownedItemPredicate+` AND execution_state='COPYING'`,
		ownedItemArguments(before.Before)...,
	)
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}

func materialScope(before application.MaterialSnapshot) recordstore.Scope {
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
	where += ` AND relative_path=? AND size_bytes=? AND source_facts_digest=? AND state=? AND blob_id IS ? AND
state='DISCOVERED'`
	if source.Key.Kind != "" {
		where += ` AND COALESCE(media_type,'')=? AND width_px IS ? AND height_px IS ? AND warning_code IS ?`
		args = append(args, source.MediaType, source.Width, source.Height, optionalText(before.WarningCode))
	}
	return recordstore.Scope{Where: where, Args: args}
}

func (records materialRecords) Warn(ctx context.Context, change application.MaterialWarning) error {
	if err := records.fence(ctx, change.Before, change.NowMS); err != nil {
		return err
	}
	warnings, err := json.Marshal(change.Warnings)
	if err != nil {
		return fmt.Errorf("encode EmulationStation media warnings: %w", err)
	}
	result, err := recordstore.UpdateEmulationstationImportItemAssets(
		ctx,
		records.executor,
		recordstore.Update{
			Set:    `state=?,warning_code=?,updated_at_ms=?`,
			Values: []any{change.State, change.Code, change.NowMS},
			Scope:  materialScope(change.Before),
		},
	)
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	result, err = recordstore.UpdateEmulationstationImportItems(
		ctx,
		records.executor,
		recordstore.Update{
			Set:    `warnings_json=?,version=version+1,updated_at_ms=?`,
			Values: []any{string(warnings), change.NowMS},
			Scope:  recordstore.Scope{Where: ownedItemPredicate, Args: ownedItemArguments(change.Before.Before)},
		},
	)
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}

func (records materialRecords) Phase(ctx context.Context, change application.PhaseChange) error {
	before := change.Before.Execution
	if err := executionRecords(records).Fence(ctx, before, change.NowMS); err != nil {
		return err
	}
	result, err := recordstore.UpdateEmulationstationImports(
		ctx,
		records.executor,
		recordstore.Update{
			Set:    `phase=?,version=version+1,updated_at_ms=?`,
			Values: []any{change.Phase, change.NowMS},
			Scope: recordstore.Scope{
				Where: `id=? AND version=? AND state='RUNNING' AND COALESCE(phase,'')=?`,
				Args:  []any{before.ImportID, before.ImportVersion, change.Before.Phase},
			},
		},
	)
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}
