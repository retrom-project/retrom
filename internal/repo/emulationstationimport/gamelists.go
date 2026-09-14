package emulationstationimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/emulationstationimport"
)

func (service *Queries) Gamelists(ctx context.Context,
	query application.GamelistQuery,
) ([]application.Gamelist, error) {
	importID, parseState, afterPath, limit := query.ImportID, query.ParseState, query.AfterPath, query.Limit
	if limit < 1 || limit > 101 {
		return nil, application.ErrInvalid
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
	result := make([]application.Gamelist, 0)
	for rows.Next() {
		var value application.Gamelist
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
		if err := decodeArray(ignored, &value.IgnoredFieldNames); err != nil {
			return nil, fmt.Errorf("decode ignored gamelist fields: %w", err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("emulationstationimport/iterate gamelists: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close import query rows: %w", err)
	}
	if err := service.requireImport(ctx, importID); err != nil {
		return nil, err
	}
	return result, nil
}
