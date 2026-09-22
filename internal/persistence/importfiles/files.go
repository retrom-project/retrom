// Package importfiles owns the normalized file boundary between transport and import.
package importfiles

import (
	"context"
	"errors"
	"fmt"

	"retrom/internal/dbexec"
)

var ErrChanged = errors.New("received import file changed")

// Receive publishes completed transport files in the caller's transaction. The
// content/path pair is immutable: retries never overwrite an existing input.
func Receive(ctx context.Context, executor dbexec.Executor, sessionID string) error {
	return receive(ctx, executor, "upload.upload_session_id=?", sessionID)
}

// ReceiveFile publishes one finalized file without rescanning the whole batch.
func ReceiveFile(ctx context.Context, executor dbexec.Executor, fileID string) error {
	return receive(ctx, executor, "upload.id=?", fileID)
}

func receive(ctx context.Context, executor dbexec.Executor, predicate, id string) error {
	_, err := executor.ExecContext(ctx, `
INSERT INTO import_files(id,upload_session_id,relative_path,blob_id,size_bytes,created_at_ms)
SELECT upload.id,upload.upload_session_id,upload.relative_path,upload.final_blob_id,blob.size_bytes,upload.updated_at_ms
FROM upload_files upload JOIN blobs blob ON blob.id=upload.final_blob_id
WHERE `+predicate+` AND upload.state='COMPLETE'
ON CONFLICT(id) DO NOTHING`, id)
	if err != nil {
		return fmt.Errorf("receive import files: %w", err)
	}
	var changed bool
	err = executor.QueryRowContext(ctx, `
SELECT EXISTS(SELECT 1 FROM upload_files upload JOIN import_files file ON file.id=upload.id
JOIN blobs blob ON blob.id=upload.final_blob_id
WHERE `+predicate+` AND upload.state='COMPLETE' AND
(file.upload_session_id<>upload.upload_session_id OR file.relative_path<>upload.relative_path
OR file.blob_id IS NOT upload.final_blob_id OR file.size_bytes<>blob.size_bytes))`, id).Scan(&changed)
	if err != nil {
		return fmt.Errorf("verify received import files: %w", err)
	}
	if changed {
		return ErrChanged
	}
	return nil
}
