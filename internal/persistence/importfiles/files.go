// Package importfiles owns the normalized file boundary between transport and import.
package importfiles

import (
	"context"
	"errors"
	"fmt"

	"retrom/internal/persistence/recordstore"

	dbapi "retrom/internal/database"
)

var ErrChanged = errors.New("received import file changed")

// Receive publishes completed transport files in the caller's transaction. The
// content/path pair is immutable: retries never overwrite an existing input.
func Receive(ctx context.Context, executor dbapi.Executor, sessionID string) error {
	return receive(ctx, executor, "upload.upload_session_id=?", sessionID)
}

// ReceiveFile publishes one finalized file without rescanning the whole batch.
func ReceiveFile(ctx context.Context, executor dbapi.Executor, fileID string) error {
	return receive(ctx, executor, "upload.id=?", fileID)
}

func receive(ctx context.Context, executor dbapi.Executor, predicate, id string) error {
	_, err := recordstore.InsertRows(
		ctx,
		executor,
		"import_files",
		`
INSERT INTO import_files(id,upload_session_id,relative_path,file_record,size_bytes,created_at_ms)
SELECT upload.id,upload.upload_session_id,upload.relative_path,upload.final_file_record,
json_extract(blob.value, '$.size_bytes'),upload.updated_at_ms
FROM upload_files upload JOIN json_each(json_array(upload.final_file_record)) blob ON blob.value IS NOT NULL
WHERE `+predicate+` AND upload.state='COMPLETE'
ON CONFLICT(id) DO NOTHING`,
		id,
	)
	if err != nil {
		return fmt.Errorf("receive import files: %w", err)
	}
	var changed bool
	err = dbapi.QueryRowContext(ctx, executor, `
SELECT EXISTS(SELECT 1 FROM upload_files upload JOIN import_files file ON file.id=upload.id
JOIN json_each(json_array(upload.final_file_record)) blob ON blob.value IS NOT NULL
WHERE `+predicate+` AND upload.state='COMPLETE' AND
(file.upload_session_id<>upload.upload_session_id OR file.relative_path<>upload.relative_path
OR file.file_record IS NOT upload.final_file_record OR file.size_bytes<>json_extract(blob.value,'$.size_bytes')))
`, id).Scan(&changed)
	if err != nil {
		return fmt.Errorf("verify received import files: %w", err)
	}
	if changed {
		return ErrChanged
	}
	return nil
}
