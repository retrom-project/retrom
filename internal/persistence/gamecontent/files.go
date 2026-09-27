package gamecontent

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/service/gamecontent"
)

func (records records) Files(ctx context.Context, uploadID string) ([]gamecontent.UploadedFile, error) {
	rows, err := records.executor.QueryContext(
		ctx,
		`
SELECT f.relative_path,
f.final_file_record,
json_extract(b.value, '$.sha256'),
json_extract(b.value, '$.size_bytes')
FROM upload_files f
JOIN json_each(json_array(f.final_file_record)) b ON b.value IS NOT NULL
WHERE f.upload_session_id=?
AND f.state='COMPLETE'
ORDER BY f.relative_path,
f.id
`,
		uploadID,
	)
	if err != nil {
		return nil, fmt.Errorf("query upload Files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]gamecontent.UploadedFile, 0)
	for rows.Next() {
		var value gamecontent.UploadedFile
		if err := rows.Scan(&value.LogicalName, &value.FileRecord, &value.SHA256, &value.SizeBytes); err != nil {
			return nil, fmt.Errorf("scan upload file: %w", err)
		}
		files = append(files, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upload Files: %w", err)
	}
	return files, nil
}
