package gameassets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/repo/dbexec"
	application "retrom/internal/service/gameassets"
)

// ReleaseScheduler keeps payload release work in the same transaction as the
// game asset mutation. The composition layer adapts the running release
// service to this persistence port.
type ReleaseScheduler interface {
	StageCandidates(context.Context, dbexec.Executor, []string) error
	ScheduleConsumption(context.Context, dbexec.Executor, string, int64) error
}

type Repository struct {
	database *sql.DB
	releases ReleaseScheduler
}

func New(database *sql.DB, releases ReleaseScheduler) *Repository {
	return &Repository{database: database, releases: releases}
}

func (repository *Repository) Upload(
	ctx context.Context, uploadFileID string,
) (application.UploadedFile, bool, error) {
	var upload application.UploadedFile
	err := repository.database.QueryRowContext(ctx, `
SELECT f.upload_session_id,
b.id,
b.sha256,
b.size_bytes
FROM upload_files f
JOIN blobs b ON b.id=f.final_blob_id
WHERE f.id=?
AND f.state='COMPLETE'
`, uploadFileID).Scan(&upload.UploadID, &upload.BlobID, &upload.Digest, &upload.SizeBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return application.UploadedFile{}, false, nil
	}
	if err != nil {
		return application.UploadedFile{}, false, fmt.Errorf("read game asset upload: %w", err)
	}
	return upload, true, nil
}

func (repository *Repository) WithWrite(
	ctx context.Context, work func(application.WriteScope) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin game asset write: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := work(writeScope{executor: tx, releases: repository.releases}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit game asset write: %w", err)
	}
	return nil
}

var _ application.Repository = (*Repository)(nil)
