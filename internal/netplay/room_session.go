package netplay

import (
	"context"
	"database/sql"
	"errors"

	"retrom/internal/dbexec"

	"retrom/internal/persistence/recordstore"
)

func (service *Service) SetSessionState(
	ctx context.Context, roomID, sessionID, profileID, target string,
) error {
	allowed := target == "PAUSED_RECONNECT" || target == "RUNNING"
	if !allowed {
		return ErrRoomConflict
	}
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("set session state transaction", err)
	}
	defer dbexec.Rollback(transaction)
	var state, host string
	if err := transaction.QueryRowContext(ctx, `
SELECT session.state,room.host_profile_id FROM netplay_sessions session
JOIN netplay_rooms room ON room.id=session.room_id
WHERE session.id=? AND session.room_id=?
`, sessionID, roomID).Scan(&state, &host); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSessionNotFound
		}
		return serviceError("set session state read", err)
	}
	if host != profileID {
		return ErrForbidden
	}
	if target == "PAUSED_RECONNECT" && state != "RUNNING" || target == "RUNNING" && state != "PAUSED_RECONNECT" {
		return ErrRoomConflict
	}
	if _, err := recordstore.UpdateNetplaySessions(ctx, transaction, recordstore.Update{
		Set: `state=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=?`,
			Args:  []any{sessionID},
		},
		Values: []any{target, now},
	}); err != nil {
		return serviceError("set session state update", err)
	}
	eventType := "PAUSED"
	if target == "RUNNING" {
		eventType = "RESUMED"
	}
	if err := appendEvent(ctx, transaction, roomID, &sessionID, &profileID, intPointer(1), eventType,
		map[string]any{"schemaVersion": 1, "fromState": state, "toState": target}, now); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return serviceError("set session state commit", err)
	}
	return nil
}

func (service *Service) prepareResync(ctx context.Context, roomID, sessionID string, cause resyncCause) error {
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("prepare resync transaction", err)
	}
	defer dbexec.Rollback(transaction)
	var fromState string
	if err := transaction.QueryRowContext(
		ctx, `SELECT state FROM netplay_sessions WHERE id=? AND room_id=?`, sessionID, roomID,
	).Scan(&fromState); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSessionNotFound
		}
		return serviceError("prepare resync state", err)
	}
	if !validResyncSource(cause, fromState) {
		return ErrRoomConflict
	}
	result, err := recordstore.UpdateNetplaySessions(ctx, transaction, recordstore.Update{
		Set: `
state='RESYNCHRONIZING',resync_count=resync_count+1,
version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=? AND room_id=? AND state=?`,
			Args:  []any{sessionID, roomID, fromState},
		},
		Values: []any{now},
	})
	if err != nil {
		return serviceError("prepare resync session", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrRoomConflict
	}
	if _, err := recordstore.UpdateNetplaySessionParticipants(ctx, transaction, recordstore.Update{
		Set: `
state='RUNTIME_READY',disconnected_at_ms=NULL,
lease_expires_at_ms=NULL,version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `netplay_session_id=? AND state IN ('CONNECTED','DISCONNECTED')`,
			Args:  []any{sessionID},
		},
		Values: []any{now},
	}); err != nil {
		return serviceError("prepare resync participants", err)
	}
	eventType := "RESUMED"
	if cause == resyncHash {
		eventType = "PAUSED"
	}
	data := map[string]any{
		"schemaVersion": 1, "fromState": fromState, "toState": "RESYNCHRONIZING", "reason": string(cause),
	}
	if err := appendEvent(
		ctx, transaction, roomID, &sessionID, nil, nil, eventType, data, now,
	); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return serviceError("prepare resync commit", err)
	}
	return nil
}

func validResyncSource(cause resyncCause, fromState string) bool {
	switch cause {
	case resyncReconnect:
		return fromState == "PAUSED_RECONNECT" || fromState == "RUNNING"
	case resyncHash:
		return fromState == "RUNNING"
	case resyncHost:
		return fromState == "PAUSED_RECONNECT"
	default:
		return false
	}
}

func (service *Service) PrepareReconnectResync(ctx context.Context, roomID, sessionID string) error {
	return service.prepareResync(ctx, roomID, sessionID, resyncReconnect)
}

func (service *Service) PrepareHashResync(ctx context.Context, roomID, sessionID string) error {
	return service.prepareResync(ctx, roomID, sessionID, resyncHash)
}

func (service *Service) PrepareHostResync(ctx context.Context, roomID, sessionID string) error {
	return service.prepareResync(ctx, roomID, sessionID, resyncHost)
}

func (service *Service) MarkDisconnected(ctx context.Context, participant SocketParticipant) error {
	now := service.clock.Now().UnixMilli()
	leaseExpires := now + service.options.ReconnectLease.Milliseconds()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("mark disconnected transaction", err)
	}
	defer dbexec.Rollback(transaction)
	if _, err := recordstore.UpdateNetplaySessionParticipants(ctx, transaction, recordstore.Update{
		Set: `
state='DISCONNECTED',disconnected_at_ms=?,lease_expires_at_ms=?,
version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `netplay_session_id=? AND profile_id=? AND state='CONNECTED'`,
			Args:  []any{participant.SessionID, participant.ProfileID},
		},
		Values: []any{now, leaseExpires, now},
	}); err != nil {
		return serviceError("mark disconnected participant", err)
	}
	result, err := recordstore.UpdateNetplaySessions(ctx, transaction, recordstore.Update{
		Set: `state='PAUSED_RECONNECT',version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND room_id=? AND state='RUNNING'`,
			Args:  []any{participant.SessionID, participant.RoomID},
		},
		Values: []any{now},
	})
	if err != nil {
		return serviceError("pause disconnected session", err)
	}
	if affected, _ := result.RowsAffected(); affected == 1 {
		if err := appendEvent(ctx, transaction, participant.RoomID, &participant.SessionID,
			&participant.ProfileID, &participant.PlayerNo, "PAUSED", map[string]any{
				"schemaVersion": 1, "fromState": "RUNNING", "toState": "PAUSED_RECONNECT",
				"reason": "PEER_DISCONNECTED",
			}, now); err != nil {
			return err
		}
	}
	if err := transaction.Commit(); err != nil {
		return serviceError("mark disconnected commit", err)
	}
	return nil
}
