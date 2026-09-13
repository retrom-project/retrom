package gamecontent

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/service/gamecontent"
)

func (records records) Files(ctx context.Context, uploadID string) ([]gamecontent.UploadedFile, error) {
	rows, err := records.executor.QueryContext(
		ctx,
		`
SELECT f.relative_path,
f.final_blob_id,
b.SHA256,
b.size_bytes
FROM upload_files f
JOIN blobs b ON b.id=f.final_blob_id
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
		if err := rows.Scan(&value.LogicalName, &value.BlobID, &value.SHA256, &value.SizeBytes); err != nil {
			return nil, fmt.Errorf("scan upload file: %w", err)
		}
		files = append(files, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upload Files: %w", err)
	}
	return files, nil
}
