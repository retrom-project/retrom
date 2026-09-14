package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	application "retrom/internal/model/libraryimport"
)

func (records sourceOwnership) readESSource(
	ctx context.Context, intent application.SourceCreationIntent,
) (application.SourceCreationSnapshot, error) {
	value := application.SourceCreationSnapshot{Kind: intent.Kind}
	err := records.executor.QueryRowContext(ctx, `SELECT source.id,source.import_id,job.id,COALESCE(job.worker_id,''),
source.execution_state,plan.state,job.state,COALESCE(collection.mapping_action,''),
source.version,plan.version,job.version,job.execution_no,job.attempt_count,
COALESCE(job.leased_until_ms,0),COALESCE(job.execution_deadline_at_ms,0),
COALESCE(collection.target_platform_instance_id,''),COALESCE(collection.target_platform_instance_version,0),
COALESCE(collection.target_platform_id,''),COALESCE(collection.target_default_core_id,''),
COALESCE(collection.target_provider_id,''),COALESCE(collection.target_id,''),
COALESCE(collection.target_dat_version_id,''),COALESCE(owner.upload_session_id,''),
COALESCE(source.library_import_job_id,''),COALESCE(source.library_import_item_id,''),
plan.root_id,plan.root_config_digest,plan.source_relative_path,plan.created_by_user_id,
plan.release_year_max,plan.mapping_version,job.max_attempts,COALESCE(job.execution_started_at_ms,0),
collection.id,collection.tag_snapshot_json,source.content_kind
FROM emulationstation_import_items source JOIN emulationstation_imports plan ON plan.id=source.import_id
JOIN jobs job ON job.id=plan.import_job_id AND job.scope_type='EMULATIONSTATION_IMPORT' AND job.scope_id=plan.id
AND job.kind='SERVER_EMULATIONSTATION_IMPORT'
JOIN emulationstation_import_collections collection ON collection.id=source.collection_id
AND collection.import_id=plan.id
LEFT JOIN server_import_upload_owners owner ON owner.kind='EMULATIONSTATION' AND owner.source_item_id=source.id
WHERE source.id=? AND source.import_id=?`, intent.ItemID, intent.ImportID).Scan(
		&value.ItemID, &value.ImportID, &value.JobID, &value.WorkerID, &value.SourceState,
		&value.ImportState, &value.JobState,
		&value.MappingAction, &value.SourceVersion, &value.ImportVersion, &value.JobVersion,
		&value.ExecutionNo, &value.Attempt,
		&value.LeaseUntilMS, &value.DeadlineMS, &value.TargetPlatformInstanceID, &value.TargetVersion,
		&value.TargetPlatformID,
		&value.TargetDefaultCoreID, &value.TargetProviderID, &value.TargetID, &value.TargetDATVersionID, &value.UploadID,
		&value.LibraryJobID, &value.LibraryItemID, &value.Frozen.RootID, &value.Frozen.RootDigest, &value.Frozen.RelativePath,
		&value.Frozen.ActorUserID, &value.Frozen.ReleaseYearMax, &value.Frozen.MappingVersion, &value.Frozen.MaxAttempts,
		&value.Frozen.StartedAtMS, &value.Frozen.CollectionID, &value.Frozen.TagSnapshotJSON, &value.Frozen.ContentKind)
	if errors.Is(err, sql.ErrNoRows) {
		return application.SourceCreationSnapshot{}, application.ErrVersionConflict
	}
	if err != nil {
		return application.SourceCreationSnapshot{}, fmt.Errorf("read ES source creation fence: %w", err)
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
