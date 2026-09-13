package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/libraryimport"
)

func (records sourceOwnership) bindESSource(ctx context.Context, change application.SourceBindingChange) error {
	if err := records.fenceESSource(ctx, change); err != nil {
		return err
	}
	before := change.Before
	result, err := recordstore.UpdateEmulationstationImportItems(ctx, records.executor, recordstore.Update{
		Set: `execution_state='VALIDATING',library_import_job_id=?,library_import_item_id=?,
version=version+1,updated_at_ms=?`,
		Values: []any{change.Created.ImportJobID, change.Item.ItemID, change.NowMS},
		Scope: recordstore.Scope{
			Where: `id=? AND import_id=? AND version=? AND execution_state='COPYING'
AND library_import_job_id IS NULL AND library_import_item_id IS NULL
AND EXISTS(SELECT 1 FROM server_import_upload_owners owner
 JOIN import_jobs imported ON imported.upload_session_id=owner.upload_session_id
 JOIN import_items item ON item.import_job_id=imported.id
 WHERE owner.kind='EMULATIONSTATION' AND owner.source_item_id=emulationstation_import_items.id
 AND owner.upload_session_id=? AND imported.id=? AND item.id=? AND item.review_handoff_kind='EMULATIONSTATION')`,
			Args: []any{
				before.ItemID, before.ImportID, before.SourceVersion, before.UploadID,
				change.Created.ImportJobID, change.Item.ItemID,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("bind ES source creation: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read ES source binding count: %w", err)
	}
	if count != 1 {
		return application.ErrVersionConflict
	}
	return nil
}

func (records sourceOwnership) fenceESSource(ctx context.Context, change application.SourceBindingChange) error {
	before := change.Before
	frozen := before.Frozen
	result, err := records.executor.ExecContext(ctx, `UPDATE jobs SET version=version WHERE id=? AND version=?
AND state='RUNNING' AND kind='SERVER_EMULATIONSTATION_IMPORT' AND scope_type='EMULATIONSTATION_IMPORT' AND scope_id=?
AND execution_no=? AND attempt_count=? AND max_attempts=? AND worker_id=?
AND leased_until_ms=? AND leased_until_ms>? AND execution_started_at_ms=?
AND execution_deadline_at_ms=? AND execution_deadline_at_ms>?
AND EXISTS(SELECT 1 FROM emulationstation_imports plan
JOIN emulationstation_import_collections collection ON collection.import_id=plan.id
WHERE plan.id=? AND plan.version=? AND plan.state='RUNNING' AND plan.import_job_id=jobs.id
AND plan.root_id=? AND plan.root_config_digest=? AND plan.source_relative_path=?
AND plan.created_by_user_id=? AND plan.release_year_max=? AND plan.mapping_version=?
AND collection.id=? AND collection.mapping_action='IMPORT' AND collection.tag_snapshot_json=?
AND collection.target_platform_instance_id=? AND collection.target_platform_instance_version=?
AND collection.target_platform_id=? AND collection.target_default_core_id=?
AND collection.target_provider_id=? AND collection.target_id=? AND COALESCE(collection.target_dat_version_id,'')=?)`,
		before.JobID, before.JobVersion, before.ImportID, before.ExecutionNo, before.Attempt,
		frozen.MaxAttempts, before.WorkerID,
		before.LeaseUntilMS, change.NowMS, frozen.StartedAtMS, before.DeadlineMS, change.NowMS,
		before.ImportID, before.ImportVersion, frozen.RootID, frozen.RootDigest, frozen.RelativePath, frozen.ActorUserID,
		frozen.ReleaseYearMax, frozen.MappingVersion, frozen.CollectionID, frozen.TagSnapshotJSON,
		before.TargetPlatformInstanceID, before.TargetVersion, before.TargetPlatformID, before.TargetDefaultCoreID,
		before.TargetProviderID, before.TargetID, before.TargetDATVersionID)
	if err != nil {
		return fmt.Errorf("fence ES source creation: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read ES source fence count: %w", err)
	}
	if count != 1 {
		return application.ErrVersionConflict
	}
	return nil
}
