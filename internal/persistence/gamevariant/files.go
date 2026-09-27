package gamevariant

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	application "retrom/internal/service/gamevariant"
)

func Files(
	ctx context.Context,
	executor dbapi.Executor,
	owner string,
	variant bool,
) ([]application.File, error) {
	query := `SELECT file.role,file.file_record,file.logical_name,json_extract(blob.value, '$.sha256'),
json_extract(blob.value, '$.size_bytes'),file.sort_order
FROM game_files file JOIN json_each(json_array(file.file_record)) blob ON blob.value IS NOT NULL WHERE
file.game_id=?
ORDER BY CASE file.role WHEN 'CONTENT' THEN 0 WHEN 'DISC' THEN 1 WHEN 'DOS_SOURCE' THEN 2 ELSE 3 END,
file.sort_order,file.logical_name`
	if variant {
		query = `SELECT file.role,file.file_record,file.logical_name,json_extract(blob.value, '$.sha256'),
json_extract(blob.value, '$.size_bytes'),file.sort_order
FROM variant_files file JOIN json_each(json_array(file.file_record)) blob ON blob.value IS NOT NULL
WHERE file.game_variant_id=?
ORDER BY file.role,file.sort_order,file.logical_name`
	}
	rows, err := executor.QueryContext(ctx, query, owner)
	if err != nil {
		return nil, fmt.Errorf("query product files: %w", err)
	}
	defer func() { cleanup.Error("close product files", rows.Close()) }()
	files := make([]application.File, 0)
	for rows.Next() {
		var file application.File
		if err := rows.Scan(
			&file.Role,
			&file.FileRecord,
			&file.LogicalName,
			&file.Digest,
			&file.SizeBytes,
			&file.SortOrder,
		); err != nil {
			return nil, fmt.Errorf("scan product file: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate product files: %w", err)
	}
	return files, nil
}

func SourceNames(source *application.Source, files []application.File) {
	for _, file := range files {
		if source.ContentLogicalName == "" && (file.Role == "CONTENT" || file.Role == "DISC" ||
			file.Role == "DOS_SOURCE" || file.Role == "PROJECT_FILE") {
			source.ContentLogicalName = file.LogicalName
		}
		if source.ValidationLogicalName == "" && (file.Role == "CONTENT" || file.Role == "DISC") {
			source.ValidationLogicalName = file.LogicalName
		}
	}
}
