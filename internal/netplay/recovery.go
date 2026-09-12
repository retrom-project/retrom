package netplay

import (
	"context"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/recordstore"

	"retrom/internal/sessionstore"
)

var errInvalidRecoveryReason = errors.New("netplay/recovery: invalid reason")

func (service *Service) Recover(ctx context.Context, reason string) error {
	if reason != "SERVER_RESTARTED" && reason != "RESTORE" {
		return errInvalidRecoveryReason
	}
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("netplay/recovery: %w", err)
	}
	defer cleanup.Rollback(transaction)
	if _, err := recordstore.UpdateNetplaySessions(ctx, transaction, recordstore.Update{
		Set: `state='FAILED',finished_at_ms=?,end_reason=?,updated_at_ms=?,version=version+1`,
		Scope: recordstore.Scope{
			Where: `state NOT IN ('FINISHED','FAILED')`,
		},
		Values: []any{now, reason, now},
	}); err != nil {
		return fmt.Errorf("netplay/recover sessions: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE play_sessions
SET state='ABANDONED',ended_at_ms=?,updated_at_ms=?,version=version+1
WHERE launch_session_id IN (
  SELECT id FROM launch_sessions WHERE netplay_session_id IS NOT NULL
)
AND state='ACTIVE'
`, now, now); err != nil {
		return fmt.Errorf("netplay/recover plays: %w", err)
	}
	if _, err := sessionstore.ChangeLaunch(ctx, transaction, recordstore.Update{
		Set: `state='REVOKED',finished_at_ms=?,updated_at_ms=?,version=version+1`,
		Scope: recordstore.Scope{
			Where: `netplay_session_id IS NOT NULL AND state IN ('CREATED','ACTIVE')`,
		},
		Values: []any{now, now},
	}); err != nil {
		return fmt.Errorf("netplay/recover launches: %w", err)
	}
	if _, err := recordstore.UpdateNetplayRooms(ctx, transaction, recordstore.Update{
		Set: `
state='ENDED',current_session_id=NULL,ended_at_ms=?,end_reason=?,
updated_at_ms=?,version=version+1
`,
		Scope: recordstore.Scope{
			Where: `state IN ('STARTING','RUNNING')`,
		},
		Values: []any{now, reason, now},
	}); err != nil {
		return fmt.Errorf("netplay/recover rooms: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("netplay/recovery: %w", err)
	}
	return nil
}
