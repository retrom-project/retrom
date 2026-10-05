package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/filestore"

	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

type ServerSourceUploads struct{ database dbapi.DB }

func NewServerSourceUploads(database dbapi.DB) *ServerSourceUploads {
	return &ServerSourceUploads{database: database}
}

func (repository *ServerSourceUploads) BlobSize(_ context.Context, fileRecord string) (int64, bool, error) {
	file, err := filestore.ParseRecord(fileRecord)
	if err != nil {
		return 0, false, fmt.Errorf("blob size: %w", err)
	}
	return file.Size, true, nil
}

func (repository *ServerSourceUploads) Insert(
	ctx context.Context,
	uploadID, sourceType string,
	files []libraryservice.PreparedReusableUploadFile,
	digest string,
	now int64,
	ownerKind, ownerItemID string,
) error {
	err := dbapi.RetryTransaction(ctx, repository.database, func(transaction dbapi.Tx) error {
		if err := InsertClonedUpload(ctx, transaction, uploadID, sourceType, files, digest, now); err != nil {
			return err
		}
		if ownerKind != "" {
			if _, err := transaction.ExecContext(ctx, `
	INSERT INTO server_import_upload_owners(upload_session_id,kind,source_item_id)
	VALUES(?,?,?)`, uploadID, ownerKind, ownerItemID); err != nil {
				return fmt.Errorf("insert server source owner: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit server source upload: %w", err)
	}
	return nil
}

func (repository *ServerSourceUploads) Present(
	ctx context.Context,
	uploadID, sourceType, manifestDigest string,
	totalBytes int64,
	totalFiles int,
) (bool, error) {
	var state, storedSourceType, storedDigest string
	var storedFiles int
	var storedBytes int64
	err := dbapi.QueryRowContext(ctx, repository.database, `
SELECT state,source_type,total_files,total_bytes,manifest_digest
FROM upload_sessions WHERE id=?
`, uploadID).Scan(&state, &storedSourceType, &storedFiles, &storedBytes, &storedDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query server source upload: %w", err)
	}
	if state != "COMPLETE" || storedSourceType != sourceType || storedFiles != totalFiles ||
		storedBytes != totalBytes || storedDigest != manifestDigest {
		return false, libraryservice.ErrInvalid
	}
	return true, nil
}

func (repository *ServerSourceUploads) Creation(
	ctx context.Context,
	uploadID, targetPlatformInstanceID, contentMode string,
) (libraryservice.ServerCreated, bool, error) {
	var created libraryservice.ServerCreated
	err := dbapi.QueryRowContext(ctx, repository.database, `
SELECT import_job.id,job.id,import_job.state,import_job.total_item_count
FROM import_jobs import_job
JOIN jobs job ON job.scope_type='IMPORT_GROUP' AND job.scope_id=import_job.id AND job.kind='IMPORT_GROUP'
WHERE import_job.upload_session_id=?
  AND import_job.target_platform_instance_id=?
  AND ((import_job.config_snapshot_json)::jsonb #>> '{contentMode}')=?
`, uploadID, targetPlatformInstanceID, contentMode).Scan(
		&created.ImportJobID, &created.JobID, &created.State, &created.ItemCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		var count int
		if err := dbapi.QueryRowContext(ctx, repository.database,
			`SELECT count(*) FROM import_jobs WHERE upload_session_id=?`, uploadID,
		).Scan(&count); err != nil {
			return libraryservice.ServerCreated{}, false, fmt.Errorf("query server source creations: %w", err)
		}
		if count != 0 {
			return libraryservice.ServerCreated{}, false, libraryservice.ErrInvalid
		}
		return libraryservice.ServerCreated{}, false, nil
	}
	if err != nil {
		return libraryservice.ServerCreated{}, false, fmt.Errorf("query server source creation: %w", err)
	}
	return created, true, nil
}
