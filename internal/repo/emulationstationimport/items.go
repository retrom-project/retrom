package emulationstationimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/dbexec"
	application "retrom/internal/service/emulationstationimport"
)

func (service *Queries) Items(
	ctx context.Context,
	filter application.ItemQuery,
) ([]application.Item, error) {
	importID, query, outcome, warning := filter.ImportID, filter.Text, filter.Outcome, filter.Warning
	collectionID, afterTitle, afterID, limit := filter.CollectionID, filter.AfterTitle, filter.AfterID, filter.Limit
	if limit < 1 || limit > 51 {
		return nil, application.ErrInvalid
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT item.id,item.title,item.collection_id,collection.display_name,
collection.target_platform_instance_id,platform.name,
item.gamelist_relative_path,item.source_flags_json,item.execution_state,item.payload_state,
item.payload_release_job_id,item.content_kind,
item.warnings_json,item.discovery_code,item.error_code,item.error_details_json,item.retryable,
item.library_import_item_id,
item.published_game_id,item.existing_game_id,item.existing_matches_json,item.updated_at_ms,
validation.status,validation.compatibility_code,validation.core_id,core.name,
validation.dependency_snapshot_json,
collection.tag_snapshot_json,
EXISTS(
 SELECT 1 FROM emulationstation_import_item_assets asset
 WHERE asset.item_id=item.id AND asset.kind='COVER' AND asset.blob_id IS NOT NULL
),
EXISTS(
 SELECT 1 FROM emulationstation_import_item_assets asset
 WHERE asset.item_id=item.id AND asset.kind='VIDEO' AND asset.blob_id IS NOT NULL
)
FROM emulationstation_import_items item
LEFT JOIN emulationstation_import_collections collection ON collection.id=item.collection_id
LEFT JOIN platform_instances platform ON platform.id=collection.target_platform_instance_id
LEFT JOIN review_drafts draft ON draft.import_item_id=item.library_import_item_id
LEFT JOIN import_item_core_validations validation ON validation.id=COALESCE(
 draft.selected_validation_id,
 (SELECT candidate.id FROM import_item_core_validations candidate
  WHERE candidate.import_item_id=item.library_import_item_id
  AND candidate.source_snapshot_id=draft.effective_source_snapshot_id
  AND candidate.target_platform_instance_id=draft.target_platform_instance_id
  ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1)
)
LEFT JOIN cores core ON core.id=validation.core_id
WHERE item.import_id=?
AND (?='' OR instr(lower(item.title),lower(?))>0)
AND (?='' OR item.execution_state=?)
AND (?='' OR EXISTS(
  SELECT 1
  FROM json_each(item.warnings_json) warning_value
  WHERE json_extract(warning_value.value,'$.code')=?
))
AND (?='' OR item.collection_id=?)
AND (?='' OR item.title>? OR (item.title=? AND item.id>?))
ORDER BY item.title,item.id
LIMIT ?`,
		importID,
		query, query,
		outcome, outcome,
		warning, warning,
		collectionID, collectionID,
		afterID, afterTitle, afterTitle, afterID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("emulationstationimport/list items: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]application.Item, 0)
	for rows.Next() {
		value, scanErr := scanItem(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("emulationstationimport/iterate items: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close item query rows: %w", err)
	}
	if err := service.requireImport(ctx, importID); err != nil {
		return nil, err
	}
	return result, nil
}

func scanItem(row dbexec.Scanner) (application.Item, error) {
	var value application.Item
	var collection, collectionName, target, targetName, kind sql.NullString
	var discovery, itemError, failureDetails, reviewItem, published, existing, payloadReleaseJob sql.NullString
	var validationStatus, compatibilityCode, coreID, coreName, dependencySnapshot sql.NullString
	var warnings, sourceFlags, existingMatches, tagSnapshot string
	var retryable, hasCover, hasVideo int
	if err := row.Scan(
		&value.ID, &value.Title, &collection, &collectionName, &target, &targetName,
		&value.GamelistRelativePath, &sourceFlags, &value.ExecutionState, &value.PayloadState, &payloadReleaseJob,
		&kind, &warnings, &discovery, &itemError, &failureDetails,
		&retryable, &reviewItem, &published, &existing, &existingMatches, &value.UpdatedAtMS,
		&validationStatus, &compatibilityCode, &coreID, &coreName, &dependencySnapshot,
		&tagSnapshot,
		&hasCover, &hasVideo,
	); err != nil {
		return application.Item{}, fmt.Errorf("emulationstationimport/scan item: %w", err)
	}
	value.CollectionID, value.CollectionName = nullableString(collection), nullableString(collectionName)
	value.TargetPlatformInstanceID = nullableString(target)
	value.TargetPlatformInstanceName = nullableString(targetName)
	value.ContentKind, value.DiscoveryCode = nullableString(kind), nullableString(discovery)
	value.PayloadReleaseJobID = nullableString(payloadReleaseJob)
	value.ErrorCode = nullableString(itemError)
	if failureDetails.Valid {
		var details application.FailureDetails
		if err := json.Unmarshal([]byte(failureDetails.String), &details); err != nil {
			return application.Item{}, fmt.Errorf("decode failure details: %w", err)
		}
		value.FailureDetails = &details
	}
	if validationStatus.Valid && compatibilityCode.Valid {
		runtimeCheck, err := projectRuntimeCheck(validationStatus, compatibilityCode, coreID, coreName, dependencySnapshot)
		if err != nil {
			return application.Item{}, err
		}
		value.RuntimeCheck = runtimeCheck
	}
	value.ReviewItemID = nullableString(reviewItem)
	value.PublishedGameID, value.ExistingGameID = nullableString(published), nullableString(existing)
	value.Retryable = retryable == 1
	if err := json.Unmarshal([]byte(sourceFlags), &value.SourceFlags); err != nil {
		return application.Item{}, fmt.Errorf("emulationstationimport/decode source flags: %w", err)
	}
	if err := decodeArray(warnings, &value.Warnings); err != nil {
		return application.Item{}, fmt.Errorf("decode item warnings: %w", err)
	}
	if err := decodeArray(existingMatches, &value.ExistingMatches); err != nil {
		return application.Item{}, fmt.Errorf("decode existing matches: %w", err)
	}
	if err := decodeArray(tagSnapshot, &value.Tags); err != nil {
		return application.Item{}, fmt.Errorf("emulationstationimport/decode item tag snapshot: %w", err)
	}
	value.Media = application.ItemMedia{
		Cover: application.ProjectMedia(hasCover == 1, value.Warnings, "cover"),
		Video: application.ProjectMedia(hasVideo == 1, value.Warnings, "video"),
	}
	return value, nil
}
