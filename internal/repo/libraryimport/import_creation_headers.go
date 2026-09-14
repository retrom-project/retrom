package libraryimport

import (
	"context"

	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/recordstore"
)

func (records creationRecords) Header(ctx context.Context, change application.CreationHeader) error {
	if change.Queued != nil {
		if err := records.queuedHeader(ctx, change); err != nil {
			return err
		}
	} else {
		if err := records.newHeader(ctx, change); err != nil {
			return err
		}
	}
	for _, file := range change.Plan.Dispositions {
		var err error
		if change.Queued == nil {
			result, writeErr := records.transaction.ExecContext(
				ctx,
				`
INSERT INTO import_job_files(import_job_id,upload_file_id,disposition,reason_code,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?)`,
				change.ImportID,
				file.File.ID,
				file.Disposition,
				creationNullable(file.Reason),
				change.NowMS,
				change.NowMS,
			)
			err = creationMutation(result, writeErr, "insert creation file", 1)
		} else {
			result, writeErr := records.transaction.ExecContext(ctx, `
UPDATE import_job_files SET disposition=?,reason_code=?,updated_at_ms=?
WHERE import_job_id=? AND upload_file_id=? AND disposition='PENDING'`,
				file.Disposition, creationNullable(file.Reason), change.NowMS, change.ImportID, file.File.ID)
			err = creationMutation(result, writeErr, "resolve queued creation file", 1)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (records creationRecords) newHeader(ctx context.Context, change application.CreationHeader) error {
	result, err := records.transaction.ExecContext(
		ctx,
		`
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
 attempt_count,max_attempts,available_at_ms,execution_started_at_ms,finished_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'IMPORT_GROUP',?,'IMPORT_GROUP',?,1,'{}',1,'SUCCEEDED',1,2,?,?,?,?,?)`,
		change.JobID,
		change.ImportID,
		change.DedupeKey,
		change.NowMS,
		change.NowMS,
		change.NowMS,
		change.NowMS,
		change.NowMS,
	)
	if err := creationMutation(result, err, "insert creation job", 1); err != nil {
		return err
	}
	result, err = records.transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms) VALUES(?,1,?,?,?)`,
		change.JobID, change.ConfigJSON, change.ConfigDigest, change.NowMS)
	if err := creationMutation(result, err, "insert creation input", 1); err != nil {
		return err
	}
	target := change.Plan.Target
	result, err = recordstore.CreateImportJobs(
		ctx,
		records.transaction,
		`
INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,
 platform_id,
default_core_id,provider_id,target_id,dat_version_id,metadata_provider,config_snapshot_json,
 config_snapshot_digest,
state,total_item_count,running_item_count,review_pending_item_count,ignored_file_count,
 rejected_file_count,
 version,created_at_ms,updated_at_ms,completed_at_ms) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1,?,?,?)`,
		change.ImportID,
		change.Plan.Upload.ID,
		target.ID,
		target.Version,
		target.PlatformID,
		target.DefaultCoreID,
		target.ProviderID,
		target.TargetID,
		creationNullable(change.Plan.DATVersionID),
		change.Plan.Request.MetadataProvider,
		change.ConfigJSON,
		change.ConfigDigest,
		change.State,
		len(change.Plan.Groups),
		change.Running,
		change.Pending,
		change.Ignored,
		change.Rejected,
		change.NowMS,
		change.NowMS,
		change.CompletedAtMS,
	)
	if err := creationMutation(result, err, "insert creation parent", 1); err != nil {
		return err
	}
	result, err = recordstore.CreateUploadConsumptions(ctx, records.transaction, `
INSERT INTO upload_consumptions(id,upload_session_id,upload_file_id,consumer_type,consumer_id,created_at_ms)
VALUES(?,?,NULL,'IMPORT_JOB',?,?)`, change.ConsumptionID, change.Plan.Upload.ID, change.ImportID, change.NowMS)
	return creationMutation(result, err, "insert creation consumption", 1)
}

func (records creationRecords) queuedHeader(ctx context.Context, change application.CreationHeader) error {
	target := change.Plan.Target
	result, err := records.transaction.ExecContext(
		ctx,
		`
UPDATE import_jobs SET provider_id=?,target_id=?,dat_version_id=?,config_snapshot_json=?,
 config_snapshot_digest=?,
 state=?,total_item_count=?,queued_item_count=0,running_item_count=?,review_pending_item_count=?,
 published_item_count=0,discarded_item_count=0,failed_item_count=0,cancelled_item_count=0,
ignored_file_count=?,rejected_file_count=?,last_error_code=NULL,version=version+1,updated_at_ms=?,
 completed_at_ms=?
WHERE id=? AND state='RUNNING' AND version=?`,
		target.ProviderID,
		target.TargetID,
		creationNullable(change.Plan.DATVersionID),
		change.ConfigJSON,
		change.ConfigDigest,
		change.State,
		len(change.Plan.Groups),
		change.Running,
		change.Pending,
		change.Ignored,
		change.Rejected,
		change.NowMS,
		change.CompletedAtMS,
		change.ImportID,
		change.Queued.ParentVersion,
	)
	return creationMutation(result, err, "update queued creation parent", 1)
}
