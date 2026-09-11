package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
)

func (service *Service) releaseTerminalLaunches(ctx context.Context) error {
	for {
		count, err := service.releaseTerminalLaunchBatch(ctx)
		if err != nil || count == 0 {
			return err
		}
	}
}

func (service *Service) releaseTerminalLaunchBatch(ctx context.Context) (int, error) {
	tx, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("launch retirement transaction: %w", err)
	}
	defer cleanup.Rollback(tx)
	now := service.now().UnixMilli()
	ids, err := collectIDs(ctx, tx, `SELECT launch_session_id FROM launch_payload_retirements
WHERE released_at_ms IS NULL AND due_at_ms<=?
ORDER BY due_at_ms,launch_session_id LIMIT 1`, now)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := retireLaunch(ctx, tx, id, now); err != nil {
			return 0, err
		}
	}
	// The regular reconciliation stages unreferenced blobs after these transactions.
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit launch retirement: %w", err)
	}
	return len(ids), nil
}

func retireLaunch(ctx context.Context, tx *sql.Tx, id string, now int64) error {
	statements := []struct {
		query string
		args  []any
	}{
		{`UPDATE launch_sessions SET state=CASE WHEN state IN ('CREATED','ACTIVE') THEN 'EXPIRED' ELSE state END,
finished_at_ms=COALESCE(finished_at_ms,?),updated_at_ms=?,version=version+1 WHERE id=? AND state IN ('CREATED','ACTIVE')
`, []any{now, now, id}},
		{`UPDATE play_sessions SET state='ABANDONED',ended_at_ms=?,updated_at_ms=?,version=version+1
WHERE launch_session_id=? AND state='ACTIVE'`, []any{now, now, id}},
		{`DELETE FROM launch_external_files WHERE rowid IN (
 SELECT rowid FROM launch_external_files WHERE launch_session_id=? LIMIT 200
)`, []any{id}},
		{`DELETE FROM launch_content_files WHERE rowid IN (
 SELECT rowid FROM launch_content_files WHERE launch_session_id=? LIMIT 200
)`, []any{id}},
		{`UPDATE launch_payload_retirements SET released_at_ms=? WHERE launch_session_id=?
AND NOT EXISTS(SELECT 1 FROM launch_content_files WHERE launch_session_id=launch_payload_retirements.launch_session_id)
AND NOT EXISTS(SELECT 1 FROM launch_external_files WHERE launch_session_id=launch_payload_retirements.launch_session_id)
`, []any{now, id}},
	}
	for _, s := range statements {
		if _, err := tx.ExecContext(ctx, s.query, s.args...); err != nil {
			return fmt.Errorf("retire launch payload: %w", err)
		}
	}
	return nil
}
