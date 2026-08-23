package emulationstationimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/tagging"
)

const summaryQuery = `
SELECT import.id,import.root_id,import.root_label_snapshot,import.source_relative_path,import.state,import.phase,
import.scan_job_id,import.import_job_id,import.gamelist_count,import.invalid_gamelist_count,import.collection_count,
import.folder_entry_count,import.game_count,import.estimated_source_bytes,
import.mapped_collection_count,import.skipped_collection_count,
import.skipped_mapping_item_count,
import.processable_item_count,import.blocked_item_count,import.review_pending_item_count,
import.published_item_count,import.review_discarded_item_count,import.existing_item_count,
import.failed_item_count,import.cancelled_item_count,import.media_warning_count,import.discovered_cover_count,
import.discovered_video_count,import.mapping_version,import.version,import.created_by_user_id,user.display_name,
import.last_error_code,
import.retryable,
import.created_at_ms,import.updated_at_ms,import.expires_at_ms,import.completed_at_ms
FROM emulationstation_imports import JOIN users user ON user.id=import.created_by_user_id`

type rowScanner interface{ Scan(...any) error }

func scanSummary(row rowScanner) (Summary, error) {
	var result Summary
	var importJobID, phase, errorCode sql.NullString
	var retryable int
	if err := row.Scan(
		&result.ID, &result.Root.ID, &result.Root.Label, &result.SourceRelativePath, &result.State, &phase,
		&result.ScanJobID, &importJobID, &result.Counts.Gamelists, &result.Counts.InvalidGamelists,
		&result.Counts.Collections, &result.Counts.FoldersIgnored, &result.Counts.Games,
		&result.Counts.EstimatedSourceBytes, &result.Counts.MappedCollections,
		&result.Counts.SkippedCollections, &result.Counts.SkippedMapping, &result.Counts.Processable,
		&result.Counts.Blocked, &result.Counts.ReviewPending, &result.Counts.Published,
		&result.Counts.ReviewDiscarded, &result.Counts.Existing, &result.Counts.Failed,
		&result.Counts.Cancelled, &result.Counts.MediaWarnings, &result.Counts.Covers, &result.Counts.Videos,
		&result.MappingVersion, &result.Version, &result.CreatedBy.ID, &result.CreatedBy.DisplayName,
		&errorCode, &retryable, &result.CreatedAtMS, &result.UpdatedAtMS, &result.ExpiresAtMS, &result.CompletedAtMS,
	); err != nil {
		return Summary{}, fmt.Errorf("emulationstationimport/scan summary: %w", err)
	}
	result.Phase = nullableString(phase)
	result.ImportJobID = nullableString(importJobID)
	result.LastErrorCode = nullableString(errorCode)
	result.Retryable = retryable == 1
	return result, nil
}

func (service *Service) Get(ctx context.Context, importID string) (Summary, error) {
	result, err := scanSummary(service.database.QueryRowContext(ctx, summaryQuery+` WHERE import.id=?`, importID))
	if errors.Is(err, sql.ErrNoRows) {
		return Summary{}, ErrNotFound
	}
	return result, err
}

func (service *Service) List(
	ctx context.Context,
	state string,
	beforeAt int64,
	beforeID string,
	limit int,
) ([]Summary, error) {
	if limit < 1 || limit > 21 {
		return nil, ErrInvalid
	}
	conditions := []string{"1=1"}
	arguments := []any{}
	if state != "" {
		conditions = append(conditions, "import.state=?")
		arguments = append(arguments, state)
	}
	if beforeID != "" {
		conditions = append(conditions, "(import.created_at_ms<? OR (import.created_at_ms=? AND import.id<?))")
		arguments = append(arguments, beforeAt, beforeAt, beforeID)
	}
	arguments = append(arguments, limit)
	rows, err := service.database.QueryContext(
		ctx,
		summaryQuery+` WHERE `+strings.Join(
			conditions,
			" AND ",
		)+` ORDER BY import.created_at_ms DESC,import.id DESC LIMIT ?`,
		arguments...)
	if err != nil {
		return nil, fmt.Errorf("emulationstationimport/list: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]Summary, 0)
	for rows.Next() {
		value, scanErr := scanSummary(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("emulationstationimport/iterate summaries: %w", err)
	}
	return result, nil
}

func (service *Service) Gamelists(
	ctx context.Context,
	importID, parseState, afterPath string,
	limit int,
) ([]Gamelist, error) {
	if limit < 1 || limit > 101 {
		return nil, ErrInvalid
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT relative_path,parse_state,error_code,game_count,folder_count,provider_present,
ignored_fields_json,ignored_field_other_count,created_at_ms
FROM emulationstation_import_gamelists
WHERE import_id=? AND (?='' OR parse_state=?) AND (?='' OR relative_path>?)
ORDER BY relative_path LIMIT ?`, importID, parseState, parseState, afterPath, afterPath, limit)
	if err != nil {
		return nil, fmt.Errorf("emulationstationimport/list gamelists: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]Gamelist, 0)
	for rows.Next() {
		var value Gamelist
		var errorCode sql.NullString
		var provider int
		var ignored string
		if err := rows.Scan(
			&value.RelativePath, &value.ParseState, &errorCode, &value.GameCount,
			&value.FolderCount, &provider, &ignored, &value.IgnoredFieldOtherCount,
			&value.CreatedAtMS,
		); err != nil {
			return nil, fmt.Errorf("emulationstationimport/scan gamelist: %w", err)
		}
		value.ErrorCode = nullableString(errorCode)
		value.ProviderPresent = provider == 1
		value.IgnoredFieldNames = jsonStrings(ignored)
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("emulationstationimport/iterate gamelists: %w", err)
	}
	cleanup.Error("close", rows.Close())
	if err := service.requireImport(ctx, importID); err != nil {
		return nil, err
	}
	return result, nil
}

func (service *Service) Collections(
	ctx context.Context,
	importID, afterPath, afterID string,
	limit int,
) ([]Collection, error) {
	if limit < 1 || limit > 101 {
		return nil, ErrInvalid
	}
	arguments := []any{importID}
	watermark := ""
	if afterID != "" {
		watermark = ` AND (collection.gamelist_relative_path>? OR
(collection.gamelist_relative_path=? AND collection.id>?))`
		arguments = append(arguments, afterPath, afterPath, afterID)
	}
	arguments = append(arguments, limit)
	rows, err := service.database.QueryContext(ctx, `
SELECT collection.id,collection.gamelist_relative_path,collection.relative_directory,collection.display_name,
collection.game_count,collection.issue_count,collection.folder_entry_count,collection.hidden_game_count,
collection.adult_game_count,collection.extension_summary_json,collection.extension_other_count,
collection.mapping_action,
collection.target_platform_instance_id,platform.name,collection.target_default_core_id,core.name,
collection.tag_snapshot_json,plan.state
FROM emulationstation_import_collections collection
JOIN emulationstation_imports plan ON plan.id=collection.import_id
LEFT JOIN platform_instances platform ON platform.id=collection.target_platform_instance_id
LEFT JOIN cores core ON core.id=collection.target_default_core_id
WHERE collection.import_id=?`+watermark+`
ORDER BY collection.gamelist_relative_path,collection.id LIMIT ?`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("emulationstationimport/list collections: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]Collection, 0)
	collectionStates := make(map[string]string)
	for rows.Next() {
		var value Collection
		var action, platformID, platformName, coreID, coreName sql.NullString
		var importState string
		var extensions, tagSnapshot string
		if err := rows.Scan(
			&value.ID, &value.GamelistRelativePath, &value.RelativeDirectory, &value.DisplayName,
			&value.GameCount, &value.IssueCount, &value.FolderEntryCount, &value.HiddenGameCount,
			&value.AdultGameCount, &extensions, &value.ExtensionOtherCount, &action,
			&platformID, &platformName, &coreID, &coreName, &tagSnapshot, &importState,
		); err != nil {
			return nil, fmt.Errorf("emulationstationimport/scan collection: %w", err)
		}
		value.MappingAction = nullableString(action)
		value.TargetPlatformInstanceID, value.TargetPlatformInstanceName = nullableString(
			platformID,
		), nullableString(
			platformName,
		)
		value.TargetDefaultCoreID, value.TargetDefaultCoreName = nullableString(coreID), nullableString(coreName)
		if err := json.Unmarshal([]byte(extensions), &value.ExtensionSummary); err != nil || value.ExtensionSummary == nil {
			return nil, fmt.Errorf("emulationstationimport/decode extension summary: %w", err)
		}
		if err := json.Unmarshal([]byte(tagSnapshot), &value.TagSnapshot); err != nil || value.TagSnapshot == nil {
			return nil, fmt.Errorf("emulationstationimport/decode collection tag snapshot: %w", err)
		}
		collectionStates[value.ID] = importState
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("emulationstationimport/iterate collections: %w", err)
	}
	cleanup.Error("close", rows.Close())
	if err := service.requireImport(ctx, importID); err != nil {
		return nil, err
	}
	if err := service.projectCollectionTags(ctx, result, collectionStates); err != nil {
		return nil, err
	}
	return result, nil
}

func (service *Service) projectCollectionTags(
	ctx context.Context,
	collections []Collection,
	states map[string]string,
) error {
	collectionIDs := make([]string, 0, len(collections))
	for _, collection := range collections {
		if states[collection.ID] == "AWAITING_MAPPING" {
			collectionIDs = append(collectionIDs, collection.ID)
		}
	}
	activeReferences, err := service.tags.EmulationStationReferences(ctx, collectionIDs)
	if err != nil {
		return fmt.Errorf("emulationstationimport/project collection tags: %w", err)
	}
	for index := range collections {
		if states[collections[index].ID] != "AWAITING_MAPPING" {
			continue
		}
		collections[index].TagSnapshot = activeReferences[collections[index].ID]
		if collections[index].TagSnapshot == nil {
			collections[index].TagSnapshot = []tagging.Reference{}
		}
	}
	return nil
}

func (service *Service) Items(
	ctx context.Context,
	importID, query, outcome, warning, collectionID, afterTitle, afterID string,
	limit int,
) ([]Item, error) {
	if limit < 1 || limit > 51 {
		return nil, ErrInvalid
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
	result := make([]Item, 0)
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
	cleanup.Error("close", rows.Close())
	if err := service.requireImport(ctx, importID); err != nil {
		return nil, err
	}
	return result, nil
}

func (service *Service) requireImport(ctx context.Context, importID string) error {
	var present int
	if err := service.database.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM emulationstation_imports WHERE id=?)`,
		importID,
	).Scan(&present); err != nil {
		return fmt.Errorf("emulationstationimport/check import existence: %w", err)
	}
	if present == 0 {
		return ErrNotFound
	}
	return nil
}

func scanItem(row rowScanner) (Item, error) {
	var value Item
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
		return Item{}, fmt.Errorf("emulationstationimport/scan item: %w", err)
	}
	value.CollectionID, value.CollectionName = nullableString(collection), nullableString(collectionName)
	value.TargetPlatformInstanceID = nullableString(target)
	value.TargetPlatformInstanceName = nullableString(targetName)
	value.ContentKind, value.DiscoveryCode = nullableString(kind), nullableString(discovery)
	value.PayloadReleaseJobID = nullableString(payloadReleaseJob)
	value.ErrorCode = nullableString(itemError)
	if failureDetails.Valid {
		var details FailureDetails
		if json.Unmarshal([]byte(failureDetails.String), &details) == nil {
			value.FailureDetails = &details
		}
	}
	value.RuntimeCheck = projectRuntimeCheck(
		validationStatus, compatibilityCode, coreID, coreName, dependencySnapshot,
	)
	value.ReviewItemID = nullableString(reviewItem)
	value.PublishedGameID, value.ExistingGameID = nullableString(published), nullableString(existing)
	value.Retryable = retryable == 1
	if err := json.Unmarshal([]byte(sourceFlags), &value.SourceFlags); err != nil {
		return Item{}, fmt.Errorf("emulationstationimport/decode source flags: %w", err)
	}
	_ = json.Unmarshal([]byte(warnings), &value.Warnings)
	_ = json.Unmarshal([]byte(existingMatches), &value.ExistingMatches)
	if err := json.Unmarshal([]byte(tagSnapshot), &value.Tags); err != nil || value.Tags == nil {
		return Item{}, fmt.Errorf("emulationstationimport/decode item tag snapshot: %w", err)
	}
	value.Media = ItemMedia{
		Cover: mediaProjection(hasCover == 1, value.Warnings, "cover"),
		Video: mediaProjection(hasVideo == 1, value.Warnings, "video"),
	}
	return value, nil
}

func projectRuntimeCheck(
	status, code, coreID, coreName, dependencySnapshot sql.NullString,
) *RuntimeCheck {
	if !status.Valid || !code.Valid {
		return nil
	}
	result := &RuntimeCheck{
		Status: status.String, Code: code.String, CoreID: coreID.String, CoreName: coreName.String,
		MissingEntries: make([]string, 0), MismatchedEntries: make([]string, 0),
		Dependencies: make([]RuntimeDependency, 0), BIOS: make([]RuntimeBIOS, 0),
		MissingDiscs: make([]RuntimeMissingDisc, 0),
	}
	if !dependencySnapshot.Valid || dependencySnapshot.String == "" {
		return result
	}
	var snapshot struct {
		Machine           *string  `json:"machine"`
		MissingEntries    []string `json:"missingEntries"`
		MismatchedEntries []string `json:"mismatchedEntries"`
		Dependencies      []struct {
			Kind                string   `json:"kind"`
			Machine             string   `json:"machine"`
			RequiredBy          *string  `json:"requiredBy"`
			ExpectedLogicalName string   `json:"expectedLogicalName"`
			State               string   `json:"state"`
			RequiredEntries     []string `json:"requiredEntries"`
		} `json:"dependencies"`
		BIOS []struct {
			LogicalName        string  `json:"logicalName"`
			RequirementMode    string  `json:"requirementMode"`
			ConditionCode      *string `json:"conditionCode"`
			InstallationStatus *string `json:"installationStatus"`
		} `json:"bios"`
		MultiDisc *struct {
			MissingEntries []struct {
				Ordinal         int64  `json:"ordinal"`
				SourceReference string `json:"sourceReference"`
			} `json:"missingEntries"`
		} `json:"multiDisc"`
	}
	if err := json.Unmarshal([]byte(dependencySnapshot.String), &snapshot); err != nil {
		return result
	}
	result.Machine = snapshot.Machine
	result.MissingEntries = append(result.MissingEntries, snapshot.MissingEntries...)
	result.MismatchedEntries = append(result.MismatchedEntries, snapshot.MismatchedEntries...)
	for _, dependency := range snapshot.Dependencies {
		result.Dependencies = append(result.Dependencies, RuntimeDependency{
			Kind: dependency.Kind, Machine: dependency.Machine, RequiredBy: dependency.RequiredBy,
			ExpectedLogicalName: dependency.ExpectedLogicalName, State: dependency.State,
			RequiredEntries: append([]string(nil), dependency.RequiredEntries...),
		})
		if result.Dependencies[len(result.Dependencies)-1].RequiredEntries == nil {
			result.Dependencies[len(result.Dependencies)-1].RequiredEntries = []string{}
		}
	}
	for _, dependency := range snapshot.BIOS {
		result.BIOS = append(result.BIOS, RuntimeBIOS{
			LogicalName: dependency.LogicalName, RequirementMode: dependency.RequirementMode,
			ConditionCode: dependency.ConditionCode, InstallationStatus: dependency.InstallationStatus,
		})
	}
	if snapshot.MultiDisc != nil {
		for _, missing := range snapshot.MultiDisc.MissingEntries {
			result.MissingDiscs = append(result.MissingDiscs, RuntimeMissingDisc{
				Ordinal: missing.Ordinal, SourceReference: missing.SourceReference,
			})
		}
	}
	return result
}

func mediaProjection(present bool, warnings []map[string]any, field string) string {
	for _, warning := range warnings {
		warningField, _ := warning["field"].(string)
		pathKind, _ := warning["pathKind"].(string)
		code, _ := warning["code"].(string)
		mediaWarning := strings.HasPrefix(code, "EMULATIONSTATION_IMAGE_") ||
			strings.HasPrefix(code, "EMULATIONSTATION_VIDEO_") ||
			strings.HasPrefix(code, "EMULATIONSTATION_MEDIA_")
		matchesKind := (field == "cover" && pathKind == "COVER") ||
			(field == "video" && pathKind == "VIDEO")
		if (warningField == field || matchesKind) && mediaWarning {
			return "WARNING"
		}
	}
	if present {
		return "READY"
	}
	return "MISSING"
}
