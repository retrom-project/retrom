package recordstore

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
)

func CopyArchiveFacts(ctx context.Context, db dbapi.Executor, from, to string) error {
	_, err := db.ExecContext(
		ctx,
		`INSERT INTO archive_entries(archive_file_record,ordinal,original_relative_path,normalized_path,
ascii_casefold_path,archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,
sha1,sha256,created_at_ms)
SELECT ?,ordinal,original_relative_path,normalized_path,ascii_casefold_path,archive_format,
compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,created_at_ms FROM archive_entries
WHERE archive_file_record=?
ON CONFLICT(archive_file_record,ordinal) DO NOTHING`,
		to,
		from,
	)
	if err != nil {
		return fmt.Errorf("copy archive facts to owned file: %w", err)
	}
	return nil
}
