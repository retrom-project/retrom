package emulationstationimport

import (
	"context"
	"database/sql"
	"fmt"
)

const expiredCancellationScope = `
SELECT import.id
FROM emulationstation_imports import
JOIN jobs job ON job.id=import.import_job_id
WHERE import.state='CANCEL_REQUESTED'
AND job.state='CANCEL_REQUESTED'
AND job.leased_until_ms IS NOT NULL
AND job.leased_until_ms<=?`

func recoverCancelledExecutions(ctx context.Context, transaction *sql.Tx, now int64) error {
	if _, err := transaction.ExecContext(ctx, `
UPDATE emulationstation_import_items
SET execution_state='CANCELLED',error_code='CANCELLED',retryable=0,
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE import_id IN (`+expiredCancellationScope+`)
AND execution_state IN ('PENDING','COPYING','VALIDATING')
`, now, now, now); err != nil {
		return fmt.Errorf("emulationstationimport/recover cancelled items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT job.id,'EMULATIONSTATION_IMPORT',import.id,'CANCELLED',
'{"schemaVersion":1,"recovered":true}',?
FROM emulationstation_imports import
JOIN jobs job ON job.id=import.import_job_id
WHERE import.id IN (`+expiredCancellationScope+`)
`, now, now); err != nil {
		return fmt.Errorf("emulationstationimport/recover cancelled event: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='CANCELLED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
worker_id=NULL,error_code=NULL,error_retryable=NULL,version=version+1,updated_at_ms=?
WHERE id IN (
 SELECT import.import_job_id FROM emulationstation_imports import
 WHERE import.id IN (`+expiredCancellationScope+`)
)
`, now, now, now); err != nil {
		return fmt.Errorf("emulationstationimport/recover cancelled job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, recoveredCancelledAggregateSQL, now, now, now); err != nil {
		return fmt.Errorf("emulationstationimport/recover cancelled import: %w", err)
	}
	return nil
}

const recoveredCancelledAggregateSQL = `
UPDATE emulationstation_imports
SET state='CANCELLED',phase=NULL,
skipped_mapping_item_count=(SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='SKIPPED_MAPPING'),
review_pending_item_count=(SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='REVIEW_PENDING'),
published_item_count=(SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='PUBLISHED'),
review_discarded_item_count=(SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='REVIEW_DISCARDED'),
existing_item_count=(SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='SKIPPED_EXISTING'),
blocked_item_count=(SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id
 AND item.execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT')),
failed_item_count=(SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id
 AND item.execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')),
cancelled_item_count=(SELECT count(*) FROM emulationstation_import_items item
 WHERE item.import_id=emulationstation_imports.id AND item.execution_state='CANCELLED'),
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE state='CANCEL_REQUESTED' AND import_job_id IN (
 SELECT job.id FROM jobs job WHERE job.scope_type='EMULATIONSTATION_IMPORT'
 AND job.state='CANCELLED' AND job.finished_at_ms=?
)
`
