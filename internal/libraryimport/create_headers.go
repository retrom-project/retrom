package libraryimport

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
)

const insertImportGroupJobSQL = `
INSERT INTO jobs(
  id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
  attempt_count,max_attempts,available_at_ms,execution_started_at_ms,finished_at_ms,created_at_ms,updated_at_ms
) VALUES(?,'IMPORT_GROUP',?,'IMPORT_GROUP',?,1,'{}',1,'SUCCEEDED',1,2,?,?,?,?,?)
`

const insertImportJobSQL = `
INSERT INTO import_jobs(
  id,upload_session_id,target_platform_instance_id,platform_instance_version,platform_id,
  default_core_id,core_artifact_id,dat_version_id,metadata_provider,config_snapshot_json,
  config_snapshot_digest,state,total_item_count,running_item_count,review_pending_item_count,
  ignored_file_count,rejected_file_count,version,created_at_ms,updated_at_ms,completed_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1,?,?,?)
`

func (run *creationRun) insertJob(configJSON []byte, configDigest string) error {
	dedupe := sha256.Sum256([]byte("import:" + run.plan.request.UploadID))
	_, err := run.transaction.ExecContext(
		run.ctx, insertImportGroupJobSQL, run.jobID, run.importID, hex.EncodeToString(dedupe[:]),
		run.now, run.now, run.now, run.now, run.now,
	)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	_, err = run.transaction.ExecContext(run.ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,1,?,?,?)
`, run.jobID, string(configJSON), configDigest, run.now)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	return nil
}

func (run *creationRun) insertImport(configJSON []byte, configDigest string) error {
	ignored, rejected := countDispositions(run.plan.dispositions)
	run.rejected = rejected
	run.progress = newInitialImportProgress(run.plan.request.MetadataProvider, len(run.plan.groups), rejected)
	run.resultState = run.progress.state
	if run.progress.completed {
		run.completedAt = run.now
	}
	target := run.plan.target
	_, err := run.transaction.ExecContext(
		run.ctx,
		insertImportJobSQL,
		run.importID, run.plan.request.UploadID, run.plan.request.TargetPlatformInstanceID,
		target.instanceVersion, target.platformID, target.coreID, target.artifactID, nullable(run.plan.datID),
		run.plan.request.MetadataProvider, string(configJSON), configDigest, run.progress.state,
		len(run.plan.groups), run.progress.runningItems, run.progress.reviewPendingItems,
		ignored, rejected, run.now, run.now, run.completedAt,
	)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	if err := run.insertUploadConsumption(); err != nil {
		return err
	}
	return run.insertDispositions()
}

func countDispositions(dispositions []preparedDisposition) (int, int) {
	ignored, rejected := 0, 0
	for _, disposition := range dispositions {
		switch disposition.disposition {
		case "IGNORED":
			ignored++
		case "REJECTED":
			rejected++
		}
	}
	return ignored, rejected
}

func (run *creationRun) insertUploadConsumption() error {
	consumptionID, _ := uuid.NewV7()
	_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO upload_consumptions(
  id,upload_session_id,upload_file_id,consumer_type,consumer_id,created_at_ms
) VALUES(?,?,NULL,'IMPORT_JOB',?,?)
`, consumptionID.String(), run.plan.request.UploadID, run.importID, run.now)
	if err != nil {
		return ErrInvalid
	}
	return nil
}

func (run *creationRun) insertDispositions() error {
	for _, disposition := range run.plan.dispositions {
		_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO import_job_files(
  import_job_id,upload_file_id,disposition,reason_code,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?)
`, run.importID, disposition.file.id, disposition.disposition, nullableText(disposition.reason), run.now, run.now)
		if err != nil {
			return fmt.Errorf("libraryimport/service: %w", err)
		}
	}
	return nil
}
