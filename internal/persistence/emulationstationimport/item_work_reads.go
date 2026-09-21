package emulationstationimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/dbexec"
	application "retrom/internal/service/emulationstationimport"
)

const executionItemSQL = `SELECT item.id,item.import_id,item.version,item.execution_state,
collection.target_platform_instance_id,collection.target_platform_id,COALESCE(collection.target_dat_version_id,''),
item.metadata_json,item.content_kind,collection.tag_snapshot_json,
COALESCE(item.library_import_job_id,''),COALESCE(item.library_import_item_id,'')
FROM emulationstation_import_items item
JOIN emulationstation_import_collections collection ON collection.id=item.collection_id`

func (records itemWorkRecords) Next(ctx context.Context, id string) (application.ExecutionItem, bool, error) {
	item, found, err := readExecutionItem(records.executor.QueryRowContext(ctx, executionItemSQL+`
WHERE item.import_id=? AND item.execution_state IN ('PENDING','COPYING','VALIDATING')
AND collection.mapping_action='IMPORT'
ORDER BY CASE item.execution_state WHEN 'PENDING' THEN 1 ELSE 0 END,
item.gamelist_relative_path,item.game_ordinal,item.id LIMIT 1`, id))
	if err != nil || !found {
		return application.ExecutionItem{}, false, err
	}
	if err := records.materials(ctx, &item); err != nil {
		return application.ExecutionItem{}, false, err
	}
	return item, true, nil
}

func (records itemWorkRecords) Item(ctx context.Context, id string) (application.OwnedItem, error) {
	item, found, err := readExecutionItem(records.executor.QueryRowContext(ctx, executionItemSQL+` WHERE item.id=?`, id))
	if err != nil {
		return application.OwnedItem{}, err
	}
	if !found {
		return application.OwnedItem{}, application.ErrNotFound
	}
	var jobID string
	if err := records.executor.QueryRowContext(
		ctx, `SELECT import_job_id FROM emulationstation_imports WHERE id=?`,
		item.ImportID,
	).Scan(

		&jobID,
	); err != nil {
		return application.OwnedItem{}, fmt.Errorf("read EmulationStation item job: %w", err)
	}
	execution, found, err := records.Current(ctx, jobID)
	if err != nil {
		return application.OwnedItem{}, err
	}
	if !found {
		return application.OwnedItem{}, application.ErrVersionConflict
	}
	return application.OwnedItem{Execution: execution, Item: item}, nil
}

func readExecutionItem(row dbexec.Scanner) (application.ExecutionItem, bool, error) {
	var item application.ExecutionItem
	var tags string
	err := row.Scan(
		&item.ID,
		&item.ImportID,
		&item.Version,
		&item.State,
		&item.TargetPlatformID,
		&item.TargetPlatformKind,
		&item.TargetDATVersionID,
		&item.MetadataJSON,
		&item.ContentKind,
		&tags,
		&item.LibraryImportJobID,
		&item.LibraryImportItemID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ExecutionItem{}, false, nil
	}
	if err != nil {
		return application.ExecutionItem{}, false, fmt.Errorf("read EmulationStation execution item: %w", err)
	}
	references := []struct {
		TagID string `json:"tagId"`
	}{}
	if err := json.Unmarshal([]byte(tags), &references); err != nil {
		return application.ExecutionItem{}, false, fmt.Errorf("decode EmulationStation item tag snapshot: %w", err)
	}
	if references == nil {
		return application.ExecutionItem{}, false, application.ErrInvalid
	}
	item.TagIDs = []string{}
	for _, reference := range references {
		item.TagIDs = append(item.TagIDs, reference.TagID)
	}
	return item, true, nil
}

func (records itemWorkRecords) materials(ctx context.Context, item *application.ExecutionItem) error {
	var err error
	item.Files, err = loadExecutionFiles(ctx, records.executor, item.ID)
	if err != nil {
		return err
	}
	item.Assets, err = loadExecutionAssets(ctx, records.executor, item.ID)
	return err
}
