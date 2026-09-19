package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/dbexec"
	tagpersistence "retrom/internal/repo/tagging"
)

type (
	ImportAdmissions struct{ database *sql.DB }
	admissionRecords struct{ executor dbexec.Executor }
)

func NewImportAdmissions(database *sql.DB) *ImportAdmissions {
	return &ImportAdmissions{database: database}
}

func (repository *ImportAdmissions) ReadAdmissionFacts(
	ctx context.Context, request application.ImportRequest,
) (application.ImportAdmissionFacts, error) {
	facts := BindImportFacts(repository.database)
	upload, found, err := facts.Upload(ctx, request.UploadID)
	if err != nil {
		return application.ImportAdmissionFacts{}, fmt.Errorf("read admission upload: %w", err)
	}
	if !found || upload.State != "COMPLETE" || upload.Version < 1 {
		return application.ImportAdmissionFacts{}, application.ErrInvalid
	}
	target, err := application.ReadImportTarget(ctx, facts, request.TargetPlatformInstanceID)
	if err != nil {
		return application.ImportAdmissionFacts{}, err
	}
	files, err := facts.Files(ctx, request.UploadID)
	if err != nil {
		return application.ImportAdmissionFacts{}, fmt.Errorf("read admission source files: %w", err)
	}
	if len(files) == 0 || int64(len(files)) != upload.FileCount {
		return application.ImportAdmissionFacts{}, application.ErrInvalid
	}
	snapshot, provisional, err := application.SnapshotImportTarget(ctx, facts, target)
	if err != nil {
		return application.ImportAdmissionFacts{}, err
	}
	return application.ImportAdmissionFacts{
		Upload: upload, Target: provisional, TargetSnapshot: snapshot, Files: files,
	}, nil
}

func (repository *ImportAdmissions) CommitAdmission(
	ctx context.Context, change application.ImportAdmissionChange,
) error {
	return dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		tags := tagpersistence.BindCrossDomain(executor)
		validTags, err := tags.ValidateActiveReferences(ctx, change.Request.TagIDs)
		if err != nil {
			return fmt.Errorf("validate admission tags: %w", err)
		}
		if len(validTags) > 0 {
			documents, err := application.BuildAdmissionDocuments(change, validTags)
			if err != nil {
				return err
			}
			change.Documents = documents
		}
		records := admissionRecords{executor: executor}
		if err := records.fence(ctx, change); err != nil {
			return err
		}
		if err := records.Create(ctx, change); err != nil {
			return fmt.Errorf("persist admitted import: %w", err)
		}
		return nil
	})
}

func (records admissionRecords) Create(ctx context.Context, change application.ImportAdmissionChange) error {
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
	result, err := records.executor.ExecContext(ctx, `
UPDATE upload_sessions SET version=version
WHERE id=? AND state='COMPLETE' AND version=? AND manifest_digest=? AND total_files=?`,
		upload.ID, upload.Version, upload.ManifestDigest, upload.FileCount)
	if err := admissionFenceResult(result, err); err != nil {
		return fmt.Errorf("fence import upload: %w", err)
	}
	target := change.Target
	result, err = records.executor.ExecContext(ctx, `
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
