package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/persistence/dbexec"
	application "retrom/internal/service/libraryimport"
)

type SourceResults struct{ executor dbexec.Executor }

func BindSourceResults(executor dbexec.Executor) *SourceResults {
	return &SourceResults{executor: executor}
}

func (records *SourceResults) Read(
	ctx context.Context,
	created application.ServerCreated,
) (application.ServerImportResult, error) {
	return records.ReadItem(ctx, created, "")
}

func (records *SourceResults) ReadItem(
	ctx context.Context,
	created application.ServerCreated,
	itemID string,
) (application.ServerImportResult, error) {
	result := application.ServerImportResult{Created: created}
	rows, err := records.executor.QueryContext(ctx, `
SELECT item.id,item.state,COALESCE(validation.status,''),COALESCE(validation.compatibility_code,''),
COALESCE(validation.core_id,''),COALESCE(core.name,''),COALESCE(validation.dependency_snapshot_json,''),
snapshot.content_kind,snapshot.source_manifest_json,snapshot.source_manifest_digest,
COALESCE(duplicate.existing_game_id,''),
COALESCE((SELECT json_group_array(relative_path) FROM (
 SELECT DISTINCT upload.relative_path AS relative_path
 FROM import_item_source_files source JOIN upload_files upload ON upload.id=source.upload_file_id
 WHERE source.import_item_id=item.id AND source.role IN ('CONTENT','DOS_SOURCE','PLAYLIST_SOURCE','DISC','PROJECT_FILE')
 ORDER BY upload.relative_path
)),'[]')
FROM import_items item
JOIN import_item_source_snapshots snapshot ON snapshot.import_item_id=item.id AND snapshot.created_by='IDENTIFICATION'
LEFT JOIN review_drafts draft ON draft.import_item_id=item.id
LEFT JOIN import_item_core_validations validation ON validation.id=COALESCE(
 draft.selected_validation_id,
 (SELECT candidate.id FROM import_item_core_validations candidate
  WHERE candidate.import_item_id=item.id
  AND candidate.source_snapshot_id=draft.effective_source_snapshot_id
  AND candidate.target_platform_instance_id=draft.target_platform_instance_id
  ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1)
)
LEFT JOIN cores core ON core.id=validation.core_id
LEFT JOIN import_item_duplicate_matches duplicate ON duplicate.import_item_id=item.id
WHERE item.import_job_id=? AND (?='' OR item.id=?)
ORDER BY item.id,duplicate.existing_game_id
`, created.ImportJobID, itemID, itemID)
	if err != nil {
		return application.ServerImportResult{}, fmt.Errorf("libraryimport/server source: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	itemIndexes := map[string]int{}
	for rows.Next() {
		var item application.ServerImportItem
		var sourcePaths string
		if err := rows.Scan(&item.ItemID, &item.State, &item.ValidationStatus, &item.CompatibilityCode,
			&item.CoreID, &item.CoreName, &item.DependencySnapshotJSON,
			&item.ContentKind, &item.SourceManifestJSON, &item.SourceManifestDigest,
			&item.ExistingGameID, &sourcePaths); err != nil {
			return application.ServerImportResult{}, fmt.Errorf("libraryimport/server source: %w", err)
		}
		if err := json.Unmarshal([]byte(sourcePaths), &item.SourceRelativePaths); err != nil {
			return application.ServerImportResult{}, fmt.Errorf("decode server source paths: %w", err)
		}
		if index, exists := itemIndexes[item.ItemID]; exists {
			if item.ExistingGameID != "" {
				result.Items[index].ExistingMatches = append(
					result.Items[index].ExistingMatches,
					application.ServerDuplicateMatch{GameID: item.ExistingGameID},
				)
			}
			continue
		}
		if item.ExistingGameID != "" {
			item.ExistingMatches = append(
				item.ExistingMatches,
				application.ServerDuplicateMatch{GameID: item.ExistingGameID},
			)
		}
		itemIndexes[item.ItemID] = len(result.Items)
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return application.ServerImportResult{}, fmt.Errorf("libraryimport/server source: %w", err)
	}
	if err := rows.Close(); err != nil {
		return application.ServerImportResult{}, fmt.Errorf("close server source results: %w", err)
	}
	result.RejectedCodes, err = records.rejectedCodes(ctx, created.ImportJobID)
	if err != nil {
		return application.ServerImportResult{}, err
	}
	return result, nil
}

func (records *SourceResults) rejectedCodes(ctx context.Context, importJobID string) ([]string, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT DISTINCT COALESCE(reason_code,'IMPORT_INVALID') FROM import_job_files
WHERE import_job_id=? AND disposition='REJECTED' ORDER BY 1
`, importJobID)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/server source: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	codes := make([]string, 0)
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("libraryimport/server source: %w", err)
		}
		codes = append(codes, code)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("libraryimport/server source: %w", err)
	}
	return codes, nil
}
