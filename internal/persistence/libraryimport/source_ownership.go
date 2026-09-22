package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/dbexec"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/libraryimport"
)

type sourceOwnership struct{ executor dbexec.Executor }

func BindSourceOwnership(executor dbexec.Executor) application.SourceOwnershipRecords {
	return sourceOwnership{executor: executor}
}

func (records sourceOwnership) ReadSource(
	ctx context.Context,
	intent application.SourceCreationIntent,
) (application.SourceCreationSnapshot, error) {
	if intent.Kind != application.SourceOwnerSource {
		return application.SourceCreationSnapshot{}, application.ErrInvalid
	}
	var value application.SourceCreationSnapshot
	value.Kind = intent.Kind
	err := records.executor.QueryRowContext(ctx, `SELECT source.id,source.import_id,job.id,COALESCE(job.worker_id,''),
source.execution_state,plan.state,job.state,COALESCE(collection.mapping_action,''),
source.version,plan.version,job.version,job.execution_no,job.attempt_count,
COALESCE(job.leased_until_ms,0),COALESCE(job.execution_deadline_at_ms,0),
COALESCE(collection.target_platform_instance_id,''),COALESCE(collection.target_platform_instance_version,0),
COALESCE(collection.target_platform_id,''),COALESCE(collection.target_default_core_id,''),
COALESCE(collection.target_provider_id,''),COALESCE(collection.target_id,''),
COALESCE(collection.target_dat_version_id,''),
COALESCE(owner.upload_session_id,''),COALESCE(source.library_import_job_id,''),
COALESCE(source.library_import_item_id,'')
FROM source_import_items source JOIN source_imports plan ON plan.id=source.import_id
JOIN jobs job ON job.id=plan.import_job_id AND job.scope_type='SOURCE_IMPORT' AND job.scope_id=plan.id
 AND job.kind='IMPORT_RECEIVE'
LEFT JOIN source_import_collections collection ON collection.id=source.collection_id AND collection.import_id=plan.id
LEFT JOIN server_import_upload_owners owner ON owner.kind='SOURCE' AND owner.source_item_id=source.id
WHERE source.id=? AND source.import_id=?`, intent.ItemID, intent.ImportID).Scan(
		&value.ItemID,
		&value.ImportID,
		&value.JobID,
		&value.WorkerID,
		&value.SourceState,
		&value.ImportState,
		&value.JobState,
		&value.MappingAction,
		&value.SourceVersion,
		&value.ImportVersion,
		&value.JobVersion,
		&value.ExecutionNo,
		&value.Attempt,
		&value.LeaseUntilMS,
		&value.DeadlineMS,
		&value.TargetPlatformInstanceID, &value.TargetVersion, &value.TargetPlatformID, &value.TargetDefaultCoreID,
		&value.TargetProviderID,
		&value.TargetID,
		&value.TargetDATVersionID,
		&value.UploadID,
		&value.LibraryJobID,
		&value.LibraryItemID)
	if errors.Is(err, sql.ErrNoRows) {
		return application.SourceCreationSnapshot{}, application.ErrVersionConflict
	}
	if err != nil {
		return application.SourceCreationSnapshot{}, fmt.Errorf("read source creation fence: %w", err)
	}
	value.Files, err = records.sourceFiles(ctx, intent.Kind, intent.ItemID)
	if err != nil {
		return application.SourceCreationSnapshot{}, err
	}
	for _, file := range value.Files {
		value.PrimaryPaths = append(value.PrimaryPaths, file.File.RelativePath)
	}
	return value, nil
}

func (records sourceOwnership) sourcePaths(
	ctx context.Context, kind application.SourceOwnerKind, itemID string,
) ([]string, error) {
	table, err := sourceOwnerFilesTable(kind)
	if err != nil {
		return nil, err
	}
	rows, err := records.executor.QueryContext(ctx, `
SELECT relative_path FROM `+table+` WHERE item_id=? ORDER BY ordinal`, itemID)
	if err != nil {
		return nil, fmt.Errorf("query source primary paths: %w", err)
	}
	defer func() { cleanup.Error("close source paths", rows.Close()) }()
	result := []string{}
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("read source primary path: %w", err)
		}
		result = append(result, path)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source primary paths: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close source primary paths: %w", err)
	}
	return result, nil
}

func (records sourceOwnership) BindSource(ctx context.Context, change application.SourceBindingChange) error {
	before := change.Before
	if before.Kind != application.SourceOwnerSource {
		return application.ErrInvalid
	}
	result, err := recordstore.UpdateSourceImportItems(ctx, records.executor, recordstore.Update{
		Set: `execution_state='VALIDATING',content_kind=?,source_manifest_json=?,source_manifest_digest=?,
library_import_job_id=?,library_import_item_id=?,version=version+1,updated_at_ms=?`,
		Values: []any{
			change.Item.ContentKind,
			change.Item.SourceManifestJSON,
			change.Item.SourceManifestDigest,
			change.Created.ImportJobID,
			change.Item.ItemID,
			change.NowMS,
		},
		Scope: recordstore.Scope{
			Where: `id=? AND import_id=? AND version=? AND execution_state='COPYING'
AND library_import_job_id IS NULL AND library_import_item_id IS NULL
AND EXISTS(SELECT 1 FROM source_imports plan JOIN jobs job ON job.id=plan.import_job_id
 WHERE plan.id=source_import_items.import_id AND plan.version=? AND plan.state='RUNNING'
 AND job.id=? AND job.version=? AND job.scope_type='SOURCE_IMPORT' AND job.scope_id=plan.id
 AND job.kind='IMPORT_RECEIVE' AND job.state='RUNNING' AND job.worker_id=?
 AND job.execution_no=? AND job.attempt_count=? AND job.leased_until_ms>? AND job.execution_deadline_at_ms>?)
AND EXISTS(SELECT 1 FROM server_import_upload_owners owner
 JOIN import_jobs imported ON imported.upload_session_id=owner.upload_session_id
 JOIN import_items item ON item.import_job_id=imported.id
 WHERE owner.kind='SOURCE' AND owner.source_item_id=source_import_items.id AND owner.upload_session_id=?
 AND imported.id=? AND item.id=?)`,
			Args: []any{
				before.ItemID,
				before.ImportID,
				before.SourceVersion,
				before.ImportVersion,
				before.JobID,
				before.JobVersion,
				before.WorkerID,
				before.ExecutionNo,
				before.Attempt,
				change.NowMS,
				change.NowMS,
				before.UploadID,
				change.Created.ImportJobID,
				change.Item.ItemID,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("write source creation binding: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read source creation affected rows: %w", err)
	}
	if count != 1 {
		return application.ErrVersionConflict
	}
	return nil
}

func (records sourceOwnership) sourceFiles(
	ctx context.Context, kind application.SourceOwnerKind, itemID string,
) ([]application.SourceCreationFile, error) {
	table, err := sourceOwnerFilesTable(kind)
	if err != nil {
		return nil, err
	}
	rows, err := records.executor.QueryContext(ctx, `
SELECT relative_path,COALESCE(blob_id,''),COALESCE(size_bytes,-1),state,
COALESCE(source_facts_digest,'') FROM `+table+` WHERE item_id=? ORDER BY ordinal`, itemID)
	if err != nil {
		return nil, fmt.Errorf("query copied source files: %w", err)
	}
	defer func() { cleanup.Error("close copied source files", rows.Close()) }()
	result := []application.SourceCreationFile{}
	for rows.Next() {
		var file application.SourceCreationFile
		if err := rows.Scan(
			&file.File.RelativePath, &file.File.BlobID, &file.File.SizeBytes, &file.State, &file.FactsDigest,
		); err != nil {
			return nil, fmt.Errorf("read copied source file: %w", err)
		}
		result = append(result, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate copied source files: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close copied source files: %w", err)
	}
	return result, nil
}
