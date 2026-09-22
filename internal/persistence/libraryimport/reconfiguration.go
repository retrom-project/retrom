package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/dbexec"
	"retrom/internal/persistence/importfiles"
	"retrom/internal/persistence/storequery"
	application "retrom/internal/service/libraryimport"

	"github.com/google/uuid"
)

type Reconfigurations struct{ database *sql.DB }

func NewReconfigurations(database *sql.DB) *Reconfigurations {
	return &Reconfigurations{database: database}
}

func (repository *Reconfigurations) Source(
	ctx context.Context,
	sourceImportJobID string,
	expectedVersion int64,
) (application.ReconfigurationSource, bool, error) {
	var source application.ReconfigurationSource
	var state string
	var version int64
	err := repository.database.QueryRowContext(ctx, `
SELECT upload.source_type,import_job.state,import_job.version
FROM import_jobs import_job
JOIN upload_sessions upload ON upload.id=import_job.upload_session_id
WHERE import_job.id=?
  AND NOT EXISTS(SELECT 1 FROM (`+storequery.DiscardedImportJobs+`) discarded WHERE discarded.import_id=import_job.id)
`, sourceImportJobID).Scan(&source.SourceType, &state, &version)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return application.ReconfigurationSource{}, false, nil
		}
		return application.ReconfigurationSource{}, false, fmt.Errorf("query reconfiguration source: %w", err)
	}
	if state != "PARTIAL_FAILURE" || version != expectedVersion {
		return application.ReconfigurationSource{}, false, nil
	}
	rows, err := repository.database.QueryContext(ctx, `
SELECT upload_file.id,upload_file.relative_path,upload_file.size_bytes,upload_file.blob_id
FROM import_job_files import_file
JOIN import_files upload_file ON upload_file.id=import_file.upload_file_id
LEFT JOIN import_job_file_resolutions resolution
  ON resolution.import_job_id=import_file.import_job_id
 AND resolution.upload_file_id=import_file.upload_file_id
WHERE import_file.import_job_id=?
  AND import_file.disposition='REJECTED'
  AND resolution.upload_file_id IS NULL
ORDER BY upload_file.relative_path,upload_file.id
`, sourceImportJobID)
	if err != nil {
		return application.ReconfigurationSource{}, false, fmt.Errorf("query reconfiguration files: %w", err)
	}
	defer func() { cleanup.Error("close reconfiguration files", rows.Close()) }()
	for rows.Next() {
		var file application.PreparedReusableUploadFile
		if err := rows.Scan(&file.ID, &file.Path, &file.Size, &file.BlobID); err != nil {
			return application.ReconfigurationSource{}, false, fmt.Errorf("scan reconfiguration file: %w", err)
		}
		source.Files = append(source.Files, file)
	}
	if err := rows.Err(); err != nil {
		return application.ReconfigurationSource{}, false, fmt.Errorf("iterate reconfiguration files: %w", err)
	}
	return source, true, nil
}

func (repository *Reconfigurations) Clone(
	ctx context.Context, clone application.ReconfigurationClone,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin reconfiguration clone: %w", err)
	}
	defer dbexec.Rollback(transaction)
	var state string
	var version int64
	err = transaction.QueryRowContext(ctx, `
SELECT state,version FROM import_jobs WHERE id=?
`, clone.SourceImportJobID).Scan(&state, &version)
	if errors.Is(err, sql.ErrNoRows) || state != "PARTIAL_FAILURE" || version != clone.ExpectedVersion {
		return application.ErrInvalid
	}
	if err != nil {
		return fmt.Errorf("verify reconfiguration source: %w", err)
	}
	if err := InsertClonedUpload(ctx, transaction, clone.UploadID, clone.SourceType, clone.Files,
		clone.ManifestDigest, clone.NowMS); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit reconfiguration clone: %w", err)
	}
	return nil
}

func InsertClonedUpload(
	ctx context.Context,
	executor dbexec.Executor,
	uploadID, sourceType string,
	files []application.PreparedReusableUploadFile,
	manifestDigest string,
	now int64,
) error {
	if _, err := executor.ExecContext(ctx, `
INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,version,
expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'COMPLETE',?,?,?,?,1,?,?,?)
`, uploadID, sourceType, len(files), totalUploadBytes(files), manifestDigest,
		now+24*60*60*1000, now, now); err != nil {
		return fmt.Errorf("insert cloned upload session: %w", err)
	}
	for _, file := range files {
		fileID, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("allocate cloned upload file ID: %w", err)
		}
		if _, err := executor.ExecContext(ctx, `
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,
final_blob_id,state,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,'COMPLETE',?,?)
`, fileID.String(), uploadID, file.Path, file.Size, file.Size, file.BlobID, now, now); err != nil {
			return fmt.Errorf("insert cloned upload file: %w", err)
		}
	}
	if err := importfiles.Receive(ctx, executor, uploadID); err != nil {
		return fmt.Errorf("publish reused files: %w", err)
	}
	return nil
}

func totalUploadBytes(files []application.PreparedReusableUploadFile) int64 {
	var total int64
	for _, file := range files {
		total += file.Size
	}
	return total
}

func (repository *Reconfigurations) RemoveUnused(ctx context.Context, uploadID string) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin remove cloned upload: %w", err)
	}
	defer dbexec.Rollback(transaction)
	var consumptionCount int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM upload_consumptions WHERE upload_session_id=?
`, uploadID).Scan(&consumptionCount); err != nil {
		return fmt.Errorf("check cloned upload use: %w", err)
	}
	if consumptionCount != 0 {
		return nil
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM upload_files WHERE upload_session_id=?`, uploadID); err != nil {
		return fmt.Errorf("delete cloned upload files: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM upload_sessions WHERE id=?`, uploadID); err != nil {
		return fmt.Errorf("delete cloned upload session: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit remove cloned upload: %w", err)
	}
	return nil
}

var _ application.ReconfigurationRepository = (*Reconfigurations)(nil)
