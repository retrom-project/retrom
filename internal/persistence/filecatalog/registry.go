package filecatalog

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"time"

	"retrom/internal/persistence/recordstore"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
)

// EnsureRecord registers one independently stored file by immutable identity.
// The physical write happens before this call so callers can include the reference
// and their domain mutation in one short transaction.
func EnsureRecord(
	ctx context.Context,
	executor dbapi.Executor,
	metadata filestore.Metadata,
	mediaType string,
	createdAtMS int64,
) (string, error) {
	var id string
	_, err := recordstore.Atomic(
		ctx,
		executor,
		func(tx dbapi.Executor) (sql.Result, error) {
			// Serialize registration with domain mutations before reading the catalog.
			if _, err := tx.ExecContext(
				ctx,
				`UPDATE stored_files SET id=id WHERE id=?`,
				metadata.ID,
			); err != nil {
				return nil, fmt.Errorf("reserve Blob registration: %w", err)
			}
			if metadata.Path != "" {
				info, err := os.Stat(metadata.Path)
				if err != nil {
					return nil, fmt.Errorf("verify published Blob: %w", err)
				}
				if !info.Mode().IsRegular() || info.Size() != metadata.Size {
					return nil, fmt.Errorf("%w: publication changed", os.ErrInvalid)
				}
			}
			var err error
			id, err = ensureRecord(ctx, tx, metadata, mediaType, createdAtMS)
			return driver.RowsAffected(1), err
		},
	)
	if err != nil {
		return "", fmt.Errorf("register verified Blob: %w", err)
	}
	return id, nil
}

func ensureRecord(
	ctx context.Context,
	executor dbapi.Executor,
	metadata filestore.Metadata,
	mediaType string,
	createdAtMS int64,
) (string, error) {
	if metadata.ID == "" {
		return "", fmt.Errorf("register file: %w", os.ErrInvalid)
	}
	var existing filestore.Metadata
	err := dbapi.QueryRowContext(
		ctx,
		executor,
		`SELECT id,sha256,size_bytes,md5,sha1,crc32 FROM stored_files WHERE id=?`,
		metadata.ID,
	).
		Scan(
			&existing.ID, &existing.SHA256, &existing.Size, &existing.MD5, &existing.SHA1, &existing.CRC32)
	if err == nil {
		if existing.SHA256 != metadata.SHA256 || existing.Size != metadata.Size ||
			existing.MD5 != metadata.MD5 ||
			existing.SHA1 != metadata.SHA1 ||
			existing.CRC32 != metadata.CRC32 {
			return "", fmt.Errorf("registered file identity changed: %w", os.ErrInvalid)
		}
		return metadata.ID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("read file record: %w", err)
	}
	if metadata.Path != "" {
		info, err := os.Stat(metadata.Path)
		if err != nil {
			return "", fmt.Errorf("inspect new file: %w", err)
		}
		if time.Since(info.ModTime()) >= filestore.UnregisteredLifetime {
			return "", filestore.ErrStagingExpired
		}
	}
	_, err = executor.ExecContext(
		ctx,
		`INSERT INTO stored_files(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms,
owner_kind,owner_id) VALUES(?,?,?,?,?,?,?,?,'STAGING','')`,
		metadata.ID,
		metadata.SHA256,
		metadata.Size,
		metadata.MD5,
		metadata.SHA1,
		metadata.CRC32,
		mediaType,
		createdAtMS,
	)
	if err != nil {
		return "", fmt.Errorf("register file: %w", err)
	}
	return metadata.ID, nil
}
