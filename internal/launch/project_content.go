package launch

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
)

func (service *Service) projectProductFiles(
	ctx context.Context,
	contentRevisionID, format string,
	maximumFiles int,
) ([]lockedContentFile, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT blob_id,logical_name FROM game_content_files
WHERE game_content_revision_id=? AND role='PROJECT_FILE'
ORDER BY sort_order,logical_name
`, contentRevisionID)
	if err != nil {
		return nil, fmt.Errorf("load project product files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]lockedContentFile, 0)
	for rows.Next() {
		var file lockedContentFile
		file.Format = format
		if err := rows.Scan(&file.BlobID, &file.LogicalName); err != nil || len(files) >= maximumFiles {
			return nil, ErrBlocked
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read project product files: %w", err)
	}
	return files, nil
}
