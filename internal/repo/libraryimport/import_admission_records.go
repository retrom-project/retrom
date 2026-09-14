package libraryimport

import (
	"context"
	"fmt"

	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/recordstore"
)

func (records admissionRecords) job(ctx context.Context, change application.ImportAdmissionChange) error {
	_, err := records.transaction.ExecContext(ctx, `
INSERT INTO jobs(
 id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
 attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms
) VALUES(?,'IMPORT_GROUP',?,'IMPORT_GROUP',?,1,'{"schemaVersion":1,"inputExecutionNo":1}',1,
 'QUEUED',0,4,?,?,?)`,
		change.JobID, change.ImportID, change.Documents.DedupeKey, change.NowMS, change.NowMS, change.NowMS)
	if err != nil {
		return fmt.Errorf("insert admitted job: %w", err)
	}
	_, err = records.transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,1,?,?,?)`, change.JobID, change.Documents.InputJSON, change.Documents.InputDigest, change.NowMS)
	if err != nil {
		return fmt.Errorf("insert admitted input: %w", err)
	}
	target := change.Target
	_, err = recordstore.CreateImportJobs(ctx, records.transaction, `
INSERT INTO import_jobs(
 id,upload_session_id,target_platform_instance_id,platform_instance_version,platform_id,
 default_core_id,provider_id,target_id,dat_version_id,metadata_provider,config_snapshot_json,
 config_snapshot_digest,state,total_item_count,running_item_count,review_pending_item_count,
 ignored_file_count,rejected_file_count,version,created_at_ms,updated_at_ms,completed_at_ms
) VALUES(?,?,?,?,?,?,?,?,NULL,?,?,?,'QUEUED',0,0,0,0,0,1,?,?,NULL)`,
		change.ImportID, change.Request.UploadID, change.Request.TargetPlatformInstanceID, target.Version,
		target.PlatformID, target.DefaultCoreID, target.ProviderID, target.TargetID, change.Request.MetadataProvider,
		change.Documents.ConfigJSON, change.Documents.ConfigDigest, change.NowMS, change.NowMS)
	if err != nil {
		return fmt.Errorf("insert admitted import: %w", err)
	}
	return nil
}

func (records admissionRecords) request(ctx context.Context, change application.ImportAdmissionChange) error {
	var actor *string
	if change.ActorUserID != "" {
		actor = &change.ActorUserID
	}
	_, err := records.transaction.ExecContext(ctx, `
INSERT INTO import_group_requests(
 import_job_id,schema_version,request_json,request_digest,actor_user_id,upload_version,
 upload_manifest_digest,target_snapshot_json,target_snapshot_digest,created_at_ms
) VALUES(?,1,?,?,?,?,?,?,?,?)`, change.ImportID, change.Documents.RequestJSON, change.Documents.RequestDigest,
		actor, change.Upload.Version, change.Upload.ManifestDigest,
		change.Documents.TargetJSON, change.Documents.TargetDigest, change.NowMS)
	if err != nil {
		return fmt.Errorf("insert admitted request: %w", err)
	}
	return nil
}

func (records admissionRecords) sources(ctx context.Context, change application.ImportAdmissionChange) error {
	_, err := recordstore.CreateUploadConsumptions(ctx, records.transaction, `
INSERT INTO upload_consumptions(id,upload_session_id,upload_file_id,consumer_type,consumer_id,created_at_ms)
VALUES(?,?,NULL,'IMPORT_JOB',?,?)`, change.ConsumptionID, change.Request.UploadID, change.ImportID, change.NowMS)
	if err != nil {
		return fmt.Errorf("insert admitted consumption: %w", err)
	}
	for _, file := range change.Files {
		_, err := records.transaction.ExecContext(ctx, `
INSERT INTO import_job_files(import_job_id,upload_file_id,disposition,reason_code,created_at_ms,updated_at_ms)
VALUES(?,?,'PENDING',NULL,?,?)`, change.ImportID, file.ID, change.NowMS, change.NowMS)
		if err != nil {
			return fmt.Errorf("insert admitted source file: %w", err)
		}
	}
	return nil
}

func (records admissionRecords) event(ctx context.Context, change application.ImportAdmissionChange) error {
	_, err := records.transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_GROUP',?,'QUEUED',
 '{"schemaVersion":1,"executionNo":1,"attempt":0,"state":"QUEUED","phase":"WAITING_FOR_WORKER"}',?)`,
		change.JobID, change.ImportID, change.NowMS)
	if err != nil {
		return fmt.Errorf("insert admitted event: %w", err)
	}
	return nil
}
