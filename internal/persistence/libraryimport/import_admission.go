package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/persistence/dbexec"
	tagpersistence "retrom/internal/persistence/tagging"
	application "retrom/internal/service/libraryimport"
)

type (
	ImportAdmissions struct{ database *sql.DB }
	admissionRecords struct{ transaction *sql.Tx }
)

func NewImportAdmissions(database *sql.DB) *ImportAdmissions {
	return &ImportAdmissions{database: database}
}

func (repository *ImportAdmissions) WithAdmission(
	ctx context.Context, work func(application.ImportAdmissionScope) error,
) error {
	transaction, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin import admission: %w", err)
	}
	defer dbexec.Rollback(transaction)
	scope := application.ImportAdmissionScope{
		Facts: BindImportFacts(transaction), Tags: tagpersistence.Bind(transaction), Writer: admissionRecords{transaction},
	}
	if err := work(scope); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit import admission: %w", err)
	}
	return nil
}

func (records admissionRecords) Create(ctx context.Context, change application.ImportAdmissionChange) error {
	if err := records.fence(ctx, change); err != nil {
		return err
	}
	if err := records.job(ctx, change); err != nil {
		return err
	}
	if err := records.request(ctx, change); err != nil {
		return err
	}
	if err := records.sources(ctx, change); err != nil {
		return err
	}
	return records.event(ctx, change)
}

func (records admissionRecords) fence(ctx context.Context, change application.ImportAdmissionChange) error {
	upload := change.Upload
	result, err := records.transaction.ExecContext(ctx, `
UPDATE upload_sessions SET version=version
WHERE id=? AND state='COMPLETE' AND version=? AND manifest_digest=? AND total_files=?`,
		upload.ID, upload.Version, upload.ManifestDigest, upload.FileCount)
	if err := admissionFenceResult(result, err); err != nil {
		return fmt.Errorf("fence import upload: %w", err)
	}
	target := change.Target
	result, err = records.transaction.ExecContext(ctx, `
UPDATE platform_instances SET version=version
WHERE id=? AND enabled=1 AND deleted_at_ms IS NULL AND version=? AND default_core_id=? AND platform_id=?`,
		target.ID, target.Version, target.DefaultCoreID, target.PlatformID)
	if err := admissionFenceResult(result, err); err != nil {
		return fmt.Errorf("fence import platform instance: %w", err)
	}
	return nil
}

func admissionFenceResult(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read import admission affected rows: %w", err)
	}
	if count != 1 {
		return application.ErrVersionConflict
	}
	return nil
}
