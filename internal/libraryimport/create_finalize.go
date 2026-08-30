package libraryimport

import (
	"encoding/json"
	"fmt"

	"retrom/internal/multidisc"
	"retrom/internal/payloadrelease"
)

type duplicateProgress struct {
	state              string
	runningItems       int
	reviewPendingItems int
	completedAt        any
}

func (run *creationRun) finalize() error {
	if run.duplicateItems > 0 {
		if err := run.updateDuplicateProgress(); err != nil {
			return err
		}
	}
	if err := run.insertSucceededEvent(); err != nil {
		return err
	}
	if run.queued != nil {
		result, err := run.transaction.ExecContext(run.ctx, `
UPDATE jobs
SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
 worker_id=NULL,error_code=NULL,error_retryable=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, run.now, run.now, run.jobID, run.queued.workerID)
		if err != nil {
			return fmt.Errorf("libraryimport/service: finish queued job: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return ErrInvalid
		}
	}
	if _, err := payloadrelease.ScheduleTerminalImportJob(run.ctx, run.transaction, run.importID, run.now); err != nil {
		return fmt.Errorf("libraryimport/service: schedule aggregate release: %w", err)
	}
	if run.reconfiguration == nil {
		return nil
	}
	return recordImportReconfiguration(
		run.ctx, run.transaction, *run.reconfiguration, run.importID, run.now,
	)
}

func (run *creationRun) updateDuplicateProgress() error {
	progress := calculateDuplicateProgress(
		len(run.plan.groups)-run.duplicateItems,
		run.rejected,
		run.plan.request.MetadataProvider,
		run.now,
	)
	alreadyImportedFiles := 0
	for uploadFileID, duplicateCount := range run.duplicateCounts {
		if duplicateCount == run.sourceCounts[uploadFileID] {
			alreadyImportedFiles++
		}
	}
	run.resultState, run.completedAt = progress.state, progress.completedAt
	_, err := run.transaction.ExecContext(run.ctx, `
UPDATE import_jobs
SET state=?,running_item_count=?,review_pending_item_count=?,discarded_item_count=?,
  already_imported_item_count=?,already_imported_file_count=?,version=version+1,
  updated_at_ms=?,completed_at_ms=?
WHERE id=?
`, progress.state, progress.runningItems, progress.reviewPendingItems, run.duplicateItems,
		run.duplicateItems, alreadyImportedFiles, run.now, progress.completedAt, run.importID)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	return nil
}

func calculateDuplicateProgress(
	remainingItems int,
	rejected int,
	metadataProvider string,
	now int64,
) duplicateProgress {
	switch {
	case remainingItems == 0 && rejected > 0:
		return duplicateProgress{state: "PARTIAL_FAILURE"}
	case remainingItems == 0:
		return duplicateProgress{state: "COMPLETED", completedAt: now}
	case metadataProvider == "HASHEOUS":
		return duplicateProgress{state: "RUNNING", runningItems: remainingItems}
	case rejected > 0:
		return duplicateProgress{state: "PARTIAL_FAILURE", reviewPendingItems: remainingItems}
	default:
		return duplicateProgress{state: "REVIEW_PENDING", reviewPendingItems: remainingItems}
	}
}

func (run *creationRun) insertSucceededEvent() error {
	parserResultCode := "NOT_APPLICABLE"
	if run.plan.contentMode == multidisc.ContentKind {
		switch {
		case len(run.plan.groups) == 0:
			parserResultCode = "REJECTED"
		case run.rejected > 0:
			parserResultCode = "PARTIAL_REJECTED"
		default:
			parserResultCode = "MATCHED"
		}
	}
	executionNo, attempt := int64(1), 1
	if run.queued != nil {
		executionNo, attempt = run.queued.executionNo, run.queued.attempt
	}
	data, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "contentMode": run.plan.contentMode,
		"executionNo": executionNo, "attempt": attempt,
		"parserResultCode": parserResultCode, "itemCount": len(run.plan.groups),
		"rejectedFileCount": run.rejected,
	})
	_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_GROUP',?,'SUCCEEDED',?,?)
`, run.jobID, run.importID, string(data), run.now)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	return nil
}
