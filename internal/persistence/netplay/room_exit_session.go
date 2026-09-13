package netplay

import (
	"context"
	"fmt"

	"retrom/internal/persistence/dbexec"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/sessionstore"
)

func exitStorageError(operation string, err error) error {
	return fmt.Errorf("netplay/%s: %w", operation, err)
}

func closeNetplaySession(
	ctx context.Context,
	transaction dbexec.Executor,
	sessionID, sessionState, playState, reason string,
	now int64,
) error {
	if _, err := recordstore.UpdateNetplaySessions(ctx, transaction, recordstore.Update{
		Set: `state=?,finished_at_ms=?,end_reason=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND state NOT IN ('FINISHED','FAILED')`,
			Args:  []any{sessionID},
		},
		Values: []any{sessionState, now, reason, now},
	}); err != nil {
		return exitStorageError("finish session", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE play_sessions
SET state=?,ended_at_ms=?,updated_at_ms=?,version=version+1
WHERE launch_session_id IN (
  SELECT id FROM launch_sessions WHERE netplay_session_id=?
)
AND state='ACTIVE'
`, playState, now, now, sessionID); err != nil {
		return exitStorageError("finish session plays", err)
	}
	if _, err := sessionstore.ChangeLaunch(ctx, transaction, recordstore.Update{
		Set: `state='REVOKED',finished_at_ms=?,updated_at_ms=?,version=version+1`,
		Scope: recordstore.Scope{
			Where: `netplay_session_id=? AND state IN ('CREATED','ACTIVE')`,
			Args:  []any{sessionID},
		},
		Values: []any{now, now},
	}); err != nil {
		return exitStorageError("revoke session launches", err)
	}
	if _, err := recordstore.UpdateNetplaySessionParticipants(ctx, transaction, recordstore.Update{
		Set: `
state='LEFT',disconnected_at_ms=NULL,lease_expires_at_ms=NULL,
version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `netplay_session_id=? AND state!='LEFT'`,
			Args:  []any{sessionID},
		},
		Values: []any{now},
	}); err != nil {
		return exitStorageError("close session participants", err)
	}
	return nil
}

func endRoomRecord(ctx context.Context, transaction dbexec.Executor, roomID, reason string, now int64) error {
	if _, err := recordstore.UpdateNetplayRoomMembers(ctx, transaction, recordstore.Update{
		Set: `ready=0,left_at_ms=?,leave_reason='ROOM_ENDED',version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `room_id=? AND left_at_ms IS NULL`,
			Args:  []any{roomID},
		},
		Values: []any{now, now},
	}); err != nil {
		return exitStorageError("end room members", err)
	}
	if _, err := recordstore.UpdateNetplayRooms(ctx, transaction, recordstore.Update{
		Set: `
state='ENDED',current_session_id=NULL,ended_at_ms=?,end_reason=?,
version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=?`,
			Args:  []any{roomID},
		},
		Values: []any{now, reason, now},
	}); err != nil {
		return exitStorageError("end room", err)
	}
	return nil
}
