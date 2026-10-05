package sourceimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"retrom/internal/persistence/contentquery"
	reviewrepo "retrom/internal/persistence/libraryimport"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	application "retrom/internal/service/sourceimport"
)

func (service *Queries) Items(
	ctx context.Context,
	filter application.ItemQuery,
) ([]application.Item, error) {
	if service.readDB == nil {
		return service.items(ctx, filter)
	}
	tx, err := service.readDB.BeginTx(ctx, &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin source item read: %w", err)
	}
	defer dbapi.Rollback(tx)
	result, err := (&Queries{database: tx}).items(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("read source item facts: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit source item read: %w", err)
	}
	return result, nil
}

func (service *Queries) items(ctx context.Context, filter application.ItemQuery) ([]application.Item, error) {
	importID, query, outcome, warning := filter.ImportID, filter.Text, filter.Outcome, filter.Warning
	collectionID, afterTitle, afterID, limit := filter.CollectionID, filter.AfterTitle, filter.AfterID, filter.Limit
	if limit < 1 || limit > 51 {
		return nil, application.ErrInvalid
	}
	rows, err := service.database.QueryContext(
		ctx,
		`
SELECT item.id,item.title,item.collection_id,collection.name,
collection.target_platform_instance_id,platform.name,
item.metadata_relative_path,item.execution_state,CASE WHEN item.payload_state='RELEASING' AND
release_job.state='FAILED'
THEN 'FAILED' ELSE item.payload_state END,item.payload_release_job_id,item.content_kind,
item.warnings_json,item.source_flags_json,item.discovery_code,item.error_code,item.error_details_json,item.retryable,
item.library_import_item_id,
item.published_game_id,item.existing_game_id,item.existing_matches_json,item.updated_at_ms,
validation.status,validation.compatibility_code,validation.core_id,core.name,
validation.dependency_snapshot_json,
COALESCE(collection.tag_snapshot_json,'[]'),
EXISTS(
 SELECT 1 FROM source_import_item_assets asset
 WHERE asset.item_id=item.id AND asset.kind='COVER' AND asset.state IN ('COPIED','RELEASED')
),
EXISTS(
 SELECT 1 FROM source_import_item_assets asset
 WHERE asset.item_id=item.id AND asset.kind='VIDEO' AND asset.state IN ('COPIED','RELEASED')
)
FROM source_import_items item
LEFT JOIN jobs release_job ON release_job.id=item.payload_release_job_id
LEFT JOIN source_import_collections collection ON collection.id=item.collection_id
LEFT JOIN platform_instances platform ON platform.id=collection.target_platform_instance_id
LEFT JOIN import_items draft ON draft.id=item.library_import_item_id AND item.execution_state='REVIEW_PENDING'
LEFT JOIN (`+contentquery.CurrentContentSQL+`) validation ON validation.import_item_id=draft.id
LEFT JOIN cores core ON core.id=validation.core_id
WHERE item.import_id=?
AND (?='' OR strpos(lower(item.title),lower(?))>0)
AND (?='' OR item.execution_state=?)
AND (?='' OR EXISTS(
  SELECT 1
  FROM jsonb_array_elements_text((item.warnings_json)::jsonb) warning_value
  WHERE ((warning_value.value)::jsonb #>> '{code}')=?
))
AND (?='' OR item.collection_id=?)
AND (?='' OR item.title>? OR (item.title=? AND item.id>?))
ORDER BY item.title,item.id
LIMIT ?`,

		importID,

		query,
		query,

		outcome,
		outcome,

		warning,
		warning,

		collectionID,
		collectionID,

		afterID,
		afterTitle,
		afterTitle,
		afterID,

		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("sourceimport/list items: %w", err)
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
		return nil, fmt.Errorf("sourceimport/iterate items: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close source item read: %w", err)
	}
	for index := range result {
		item := &result[index]
		if item.ReviewItemID == nil || item.ExecutionState != "REVIEW_PENDING" {
			continue
		}
		runtime, err := reviewrepo.ReadReviewRuntime(ctx, service.database, *item.ReviewItemID)
		if err != nil {
			return nil, fmt.Errorf("read source item facts: %w", err)
		}
		text := func(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
		name := runtime.CoreID
		if item.RuntimeCheck != nil {
			name = item.RuntimeCheck.CoreName
		}
		item.RuntimeCheck,
			err = projectRuntimeCheck(
			text(runtime.Status), text(runtime.Code), text(runtime.CoreID), text(name), text(runtime.DependencyJSON),
		)
		if err != nil {
			return nil, fmt.Errorf("read source item facts: %w", err)
		}
	}
	return result, nil
}

func scanItem(row dbapi.Scanner) (application.Item, error) {
	var value application.Item
	var collection, collectionName, target, targetName, kind sql.NullString
	var discovery, itemError, failureDetails, reviewItem, published, existing, payloadReleaseJob sql.NullString
	var validationStatus, compatibilityCode, coreID, coreName, dependencySnapshot sql.NullString
	var warnings, flags, existingMatches, tagSnapshot string
	var retryable int
	var hasCover, hasVideo bool
	if err := row.Scan(
		&value.ID, &value.Title, &collection, &collectionName, &target, &targetName,
		&value.MetadataRelativePath, &value.ExecutionState, &value.PayloadState, &payloadReleaseJob,
		&kind, &warnings, &flags, &discovery, &itemError, &failureDetails,
		&retryable, &reviewItem, &published, &existing, &existingMatches, &value.UpdatedAtMS,
		&validationStatus, &compatibilityCode, &coreID, &coreName, &dependencySnapshot,
		&tagSnapshot,
		&hasCover, &hasVideo,
	); err != nil {
		return application.Item{}, fmt.Errorf("sourceimport/scan item: %w", err)
	}
	if err := json.Unmarshal([]byte(flags), &value.SourceFlags); err != nil {
		return application.Item{}, fmt.Errorf("decode source flags: %w", err)
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
		runtimeCheck, err := projectRuntimeCheck(
			validationStatus, compatibilityCode, coreID, coreName, dependencySnapshot,
		)
		if err != nil {
			return application.Item{}, err
		}
		value.RuntimeCheck = runtimeCheck
	}
	value.ReviewItemID = nullableString(reviewItem)
	value.PublishedGameID, value.ExistingGameID = nullableString(published), nullableString(existing)
	value.Retryable = retryable == 1
	if err := decodeArray(warnings, &value.Warnings); err != nil {
		return application.Item{}, fmt.Errorf("decode item warnings: %w", err)
	}
	if err := decodeArray(existingMatches, &value.ExistingMatches); err != nil {
		return application.Item{}, fmt.Errorf("decode existing matches: %w", err)
	}
	if err := decodeArray(tagSnapshot, &value.Tags); err != nil {
		return application.Item{}, fmt.Errorf("sourceimport/decode item tag snapshot: %w", err)
	}
	value.Media = application.ItemMedia{
		Cover: application.ProjectMedia(hasCover, value.Warnings, "cover"),
		Video: application.ProjectMedia(hasVideo, value.Warnings, "video"),
	}
	return value, nil
}
