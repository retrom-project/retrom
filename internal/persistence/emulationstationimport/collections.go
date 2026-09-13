package emulationstationimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	application "retrom/internal/service/emulationstationimport"
)

func (service *Queries) Collections(ctx context.Context,
	query application.CollectionQuery,
) ([]application.CollectionRecord, error) {
	importID, afterPath, afterID, limit := query.ImportID, query.AfterPath, query.AfterID, query.Limit
	if limit < 1 || limit > 101 {
		return nil, application.ErrInvalid
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
	result := make([]application.CollectionRecord, 0)
	for rows.Next() {
		var value application.CollectionRecord
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
		if err := decodeArray(extensions, &value.ExtensionSummary); err != nil {
			return nil, fmt.Errorf("emulationstationimport/decode extension summary: %w", err)
		}
		if err := decodeArray(tagSnapshot, &value.TagSnapshot); err != nil {
			return nil, fmt.Errorf("emulationstationimport/decode collection tag snapshot: %w", err)
		}
		value.ImportState = importState
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("emulationstationimport/iterate collections: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close import query rows: %w", err)
	}
	if err := service.requireImport(ctx, importID); err != nil {
		return nil, err
	}
	return result, nil
}
