package gamecontent

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/multidisc"
)

type contentIdentityFile struct {
	role, sha256 string
}

func contentReplacementUnchanged(
	ctx context.Context,
	transaction *sql.Tx,
	currentContentID string,
	prepared preparedReplacement,
) (bool, error) {
	wanted := preparedContentIdentity(prepared)
	rows, err := transaction.QueryContext(ctx, `
SELECT file.role,blob.sha256
FROM game_files file
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.game_id=?
  AND (?!='MULTI_DISC' OR file.role='DISC')
ORDER BY file.sort_order,file.role,file.logical_name
`, currentContentID, prepared.contentKind)
	if err != nil {
		return false, fmt.Errorf("query current content identity: %w", err)
	}
	defer func() { cleanup.Error("close current content identity", rows.Close()) }()
	current := make([]contentIdentityFile, 0, len(wanted))
	for rows.Next() {
		var file contentIdentityFile
		if err := rows.Scan(&file.role, &file.sha256); err != nil {
			return false, fmt.Errorf("scan current content identity: %w", err)
		}
		current = append(current, file)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate current content identity: %w", err)
	}
	if len(current) != len(wanted) {
		return false, nil
	}
	for index := range current {
		if current[index] != wanted[index] {
			return false, nil
		}
	}
	return true, nil
}

func preparedContentIdentity(prepared preparedReplacement) []contentIdentityFile {
	if prepared.contentKind == multidisc.ContentKind {
		identity := make([]contentIdentityFile, 0, len(prepared.orderedDiscSHA256))
		for _, digest := range prepared.orderedDiscSHA256 {
			identity = append(identity, contentIdentityFile{role: "DISC", sha256: digest})
		}
		return identity
	}
	identity := make([]contentIdentityFile, 0, len(prepared.files))
	for _, file := range prepared.files {
		identity = append(identity, contentIdentityFile{role: file.role, sha256: file.sha256})
	}
	return identity
}
