package jobs

import (
	"context"
	"fmt"
	"strings"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/service/jobs"
)

func (store records) CancelServerImport(ctx context.Context, change jobs.Cancellation) error {
	jobID, reason, now := change.JobID, change.Reason, change.AtMS
	pending := change.State == "CANCEL_REQUESTED"
	if pending {
		if _, err := store.executor.ExecContext(ctx, `
UPDATE server_imports SET state='CANCEL_REQUESTED',cancel_requested_at_ms=?,cancel_reason=?,
version=version+1,updated_at_ms=? WHERE job_id=? AND state='RUNNING'
`, now, strings.TrimSpace(reason), now, jobID); err != nil {
			return fmt.Errorf("jobs/server import cancel: %w", err)
		}
		return nil
	}
	var importID string
	if err := store.executor.QueryRowContext(ctx, `
SELECT id
FROM server_imports
WHERE job_id=?
`, jobID).Scan(&importID); err != nil {
		return fmt.Errorf("jobs/server import: %w", err)
	}
	if _, err := recordstore.UpdateServerBiosImportItems(ctx, store.executor, recordstore.Update{
		Set: `state='CANCELLED',outcome_code='CANCELLED',completed_at_ms=?,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `server_import_id=? AND state IN ('PENDING','EVALUATING')`,
			Args:  []any{importID},
		},
		Values: []any{now, now},
	}); err != nil {
		return fmt.Errorf("jobs/server import items: %w", err)
	}
	if _, err := store.executor.ExecContext(ctx, `
UPDATE server_imports SET state='CANCELLED',cancel_requested_at_ms=?,cancel_reason=?,
cancelled_item_count=(SELECT count(*) FROM server_bios_import_items item
WHERE item.server_import_id=server_imports.id AND item.state='CANCELLED'),
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='QUEUED'
`, now, strings.TrimSpace(reason), now, now, importID); err != nil {
		return fmt.Errorf("jobs/server import cancel: %w", err)
	}
	return nil
}
