package maintenance

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/service/maintenance"
)

func backupBlobs(ctx context.Context, database *sql.DB) ([]maintenance.Blob, error) {
	rows, err := database.QueryContext(ctx, `SELECT sha256,size_bytes FROM blobs ORDER BY sha256`)
	if err != nil {
		return nil, fmt.Errorf("list backup blobs: %w", err)
	}
	defer func() { cleanup.Error("close backup blobs", rows.Close()) }()
	var result []maintenance.Blob
	for rows.Next() {
		var blob maintenance.Blob
		if err := rows.Scan(&blob.SHA256, &blob.SizeBytes); err != nil {
			return nil, fmt.Errorf("read backup blob: %w", err)
		}
		result = append(result, blob)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate backup blobs: %w", err)
	}
	return result, nil
}

func backupParts(ctx context.Context, database *sql.DB) ([]maintenance.UploadPart, error) {
	rows, err := database.QueryContext(ctx, `SELECT p.storage_key,p.size_bytes,p.sha256 FROM upload_parts p
 JOIN upload_files f ON f.id=p.upload_file_id JOIN upload_sessions u ON u.id=f.upload_session_id
 WHERE u.state!='COMPLETE' ORDER BY p.storage_key`)
	if err != nil {
		return nil, fmt.Errorf("list backup upload parts: %w", err)
	}
	defer func() { cleanup.Error("close backup parts", rows.Close()) }()
	var result []maintenance.UploadPart
	for rows.Next() {
		var part maintenance.UploadPart
		if err := rows.Scan(&part.StorageKey, &part.SizeBytes, &part.SHA256); err != nil {
			return nil, fmt.Errorf("read backup upload part: %w", err)
		}
		result = append(result, part)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate backup upload parts: %w", err)
	}
	return result, nil
}
