package gameassets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/gameassets"
)

// ReleaseScheduler keeps payload release work in the same transaction as the
// game asset mutation. The composition layer adapts the running release
// service to this persistence port.
type ReleaseScheduler interface {
	StageCandidates(context.Context, dbapi.Executor, []string) error
	ScheduleConsumption(context.Context, dbapi.Executor, string, int64) error
}

type Repository struct {
	database dbapi.DB
	releases ReleaseScheduler
}

func New(database dbapi.DB, releases ReleaseScheduler) *Repository {
	return &Repository{database: database, releases: releases}
}

func (repository *Repository) Upload(
	ctx context.Context, uploadFileID string,
) (application.UploadedFile, bool, error) {
	var upload application.UploadedFile
	err := dbapi.QueryRowContext(ctx, repository.database, `
SELECT f.upload_session_id,
b.value,
((b.value)::jsonb #>> '{sha256}'),
(((b.value)::jsonb #>> '{size_bytes}'))::bigint
FROM upload_files f
JOIN LATERAL (SELECT f.final_file_record AS value) b ON b.value IS NOT NULL
WHERE f.id=?
AND f.state='COMPLETE'
`, uploadFileID).Scan(&upload.UploadID, &upload.FileRecord, &upload.Digest, &upload.SizeBytes)
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
	err := dbapi.RetryTransaction(ctx, repository.database, func(tx dbapi.Tx) error {
		return work(writeScope{executor: tx, releases: repository.releases})
	})
	if err != nil {
		return fmt.Errorf("commit gameassets transaction: %w", err)
	}
	return nil
}

var _ application.Repository = (*Repository)(nil)
