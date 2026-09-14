package serverimport

import (
	"context"
	"fmt"

	"retrom/internal/model/serverimport"
	"retrom/internal/repo/recordstore"
)

func (records controlRecords) Cancel(ctx context.Context, plan serverimport.Cancellation) error {
	before := plan.Before
	now := plan.Evidence.Now
	result, err := records.executor.ExecContext(ctx, `
UPDATE jobs SET state=?, cancel_requested_at_ms=?, cancel_reason=?, finished_at_ms=?, version=version+1,
updated_at_ms=? WHERE id=? AND version=? AND state=?
`, plan.State, now, plan.Reason, plan.CompletedAt, now, before.Summary.JobID, before.JobVersion, before.JobState)
	if err := requireControlChange(result, err, serverimport.ErrNotCancellable); err != nil {
		return err
	}
	if !plan.Pending {
		if _, err := recordstore.UpdateServerBiosImportItems(
			ctx,
			records.executor,
			recordstore.Update{
				Set: `state='CANCELLED',outcome_code='CANCELLED',completed_at_ms=?,updated_at_ms=?`,
				Scope: recordstore.Scope{
					Where: `server_import_id=? AND state IN ('PENDING','EVALUATING')`,
					Args: []any{
						before.Summary.ID,
					},
				},
				Values: []any{
					now,
					now,
				},
			},
		); err != nil {
			return fmt.Errorf("cancel unfinished import items: %w", err)
		}
	}
	result, err = records.executor.ExecContext(
		ctx,
		`
UPDATE server_imports SET state=?, cancel_requested_at_ms=?, cancel_reason=?, cancelled_item_count=?,
completed_at_ms=?, version=version+1, updated_at_ms=? WHERE id=? AND version=? AND state=?
`,
		plan.State,
		now,
		plan.Reason,
		plan.CancelledItems,
		plan.CompletedAt,
		now,
		before.Summary.ID,
		before.Summary.Version,
		before.Summary.State,
	)
	if err := requireControlChange(result, err, serverimport.ErrNotCancellable); err != nil {
		return err
	}
	return records.evidence(ctx, before, plan.Evidence, "CANCEL_REQUESTED", "SERVER_IMPORT_CANCEL_REQUESTED")
}
