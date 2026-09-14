package importdiscard

import (
	"context"
	"fmt"

	"retrom/internal/model/importdiscard"
)

func (writes writes) Request(ctx context.Context, request importdiscard.Request) error {
	result, err := writes.transaction.ExecContext(ctx, `INSERT INTO import_batch_discards
(kind,import_id,requested_by_user_id,state,requested_at_ms,updated_at_ms)
VALUES(?,?,?,'REQUESTED',?,?) ON CONFLICT(kind,import_id) DO UPDATE
SET state='REQUESTED',error_code=NULL,updated_at_ms=excluded.updated_at_ms
WHERE import_batch_discards.state='FAILED'`, request.Kind, request.ID, request.UserID, request.Now, request.Now)
	if err != nil {
		return fmt.Errorf("persist discard request: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count discard request: %w", err)
	}
	if count != 1 {
		return importdiscard.ErrInvalid
	}
	_, err = writes.transaction.ExecContext(ctx, `INSERT INTO audit_events
(id,actor_kind,actor_user_id,action,resource_type,resource_id,after_json,created_at_ms)
VALUES(?,'USER',?,'IMPORT_BATCH_DISCARD_REQUESTED',?,?,'{"state":"REQUESTED"}',?)`,
		request.AuditID, request.UserID, request.Kind, request.ID, request.Now)
	if err != nil {
		return fmt.Errorf("audit discard request: %w", err)
	}
	return nil
}

func (writes writes) Progress(ctx context.Context, progress importdiscard.Progress) error {
	_, err := writes.transaction.ExecContext(ctx, `UPDATE import_batch_discards SET state=?,error_code=?,
completed_at_ms=?,updated_at_ms=? WHERE kind=? AND import_id=? AND state='REQUESTED'`,
		progress.State, progress.ErrorCode, progress.CompletedAt, progress.Now, progress.Kind, progress.ID)
	if err != nil {
		return fmt.Errorf("update discard progress: %w", err)
	}
	return nil
}
