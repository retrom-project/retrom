package pegasusimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/persistence/dbexec"
	"retrom/internal/persistence/recordstore"
)

func RefreshCountsAndEvent(
	ctx context.Context,
	executor dbexec.Executor,
	jobID, importID, itemID, outcome string,
	now int64,
) error {
	if err := refreshCounts(ctx, executor, importID, now); err != nil {
		return err
	}
	data, _ := json.Marshal(map[string]any{"schemaVersion": 1, "itemId": itemID, "outcome": outcome})
	_, err := executor.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'PROGRESS',?,?)`,
		jobID,
		importID,
		string(data),
		now,
	)
	if err != nil {
		return fmt.Errorf("pegasusimport/create progress event: %w", err)
	}
	return nil
}

func refreshCounts(ctx context.Context, executor dbexec.Executor, importID string, now int64) error {
	if _, err := recordstore.UpdatePegasusImports(ctx, executor, recordstore.Update{
		Set: `
review_pending_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state='REVIEW_PENDING'
),
published_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state='PUBLISHED'
),
review_discarded_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state='REVIEW_DISCARDED'
),
existing_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state='SKIPPED_EXISTING'
),
blocked_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT')
),
failed_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')
),
cancelled_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state='CANCELLED'
),
media_warning_count=(
  SELECT count(*)
  FROM pegasus_import_items item,json_each(item.warnings_json) warning
  WHERE item.import_id=?
  AND json_extract(warning.value,'$.code') IN (
    'PEGASUS_IMAGE_INVALID','PEGASUS_VIDEO_UNSUPPORTED','PEGASUS_VIDEO_TOO_LARGE',
    'PEGASUS_MEDIA_AMBIGUOUS','PEGASUS_MEDIA_MISSING','PEGASUS_MEDIA_READ_FAILED'
  )
),
version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=?`,
			Args:  []any{importID},
		},
		Values: []any{
			importID,
			importID,
			importID,
			importID,
			importID,
			importID,
			importID,
			importID,
			now,
		},
	}); err != nil {
		return fmt.Errorf("pegasusimport/refresh aggregate counts: %w", err)
	}
	return nil
}
