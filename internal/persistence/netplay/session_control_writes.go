package netplay

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/service/netplay"
)

func (records sessionControlRecords) fence(ctx context.Context, before netplay.SessionControlSnapshot) error {
	result, err := recordstore.UpdateNetplayRooms(
		ctx,
		records.executor,
		recordstore.Update{
			Set: `version=version`,
			Scope: recordstore.Scope{
				Where: `id=? AND version=? AND current_session_id=? AND state=?`,
				Args:  []any{before.RoomID, before.RoomVersion, before.SessionID, before.RoomState},
			},
		},
	)
	return requireRoomChange(result, err, netplay.ErrRoomConflict)
}

func (records sessionControlRecords) Session(ctx context.Context, plan netplay.SessionTransitionPlan) error {
	if err := records.fence(ctx, plan.Before); err != nil {
		return err
	}
	result, err := recordstore.UpdateNetplaySessions(
		ctx,
		records.executor,
		recordstore.Update{
			Set: `state=?,resync_count=resync_count+?,
started_at_ms=CASE WHEN ? THEN COALESCE(started_at_ms,?) ELSE started_at_ms END,
version=version+1,updated_at_ms=?`,
			Scope: recordstore.Scope{
				Where: `id=? AND room_id=? AND state=? AND version=?`,
				Args:  []any{plan.Before.SessionID, plan.Before.RoomID, plan.Before.State, plan.Before.Version},
			},
			Values: []any{plan.Target, plan.IncrementResync, plan.Started, plan.Now, plan.Now},
		},
	)
	if err := requireRoomChange(result, err, netplay.ErrRoomConflict); err != nil {
		return err
	}
	if err := records.transitionPeers(ctx, plan); err != nil {
		return err
	}
	if plan.Started && plan.Before.RoomState == netplay.RoomStateStarting {
		result, err := recordstore.UpdateNetplayRooms(
			ctx,
			records.executor,
			recordstore.Update{
				Set: `state='RUNNING',version=version+1,updated_at_ms=?`,
				Scope: recordstore.Scope{
					Where: `id=? AND version=? AND current_session_id=? AND state='STARTING'`,
					Args:  []any{plan.Before.RoomID, plan.Before.RoomVersion, plan.Before.SessionID},
				},
				Values: []any{plan.Now},
			},
		)
		if err := requireRoomChange(result, err, netplay.ErrRoomConflict); err != nil {
			return err
		}
	}
	return records.events(ctx, plan.Before, plan.Events, plan.Now)
}

func (records sessionControlRecords) transitionPeers(ctx context.Context, plan netplay.SessionTransitionPlan) error {
	switch plan.PeerMode {
	case netplay.PeersUnchanged:
		return nil
	case netplay.PeersPrepareResync:
		_, err := recordstore.UpdateNetplaySessionParticipants(
			ctx,
			records.executor,
			recordstore.Update{
				Set: `state='RUNTIME_READY',disconnected_at_ms=NULL,lease_expires_at_ms=NULL,version=version+1,updated_at_ms=?`,
				Scope: recordstore.Scope{
					Where: `netplay_session_id=? AND state IN ('CONNECTED','DISCONNECTED')`,
					Args:  []any{plan.Before.SessionID},
				},
				Values: []any{plan.Now},
			},
		)
		if err != nil {
			return fmt.Errorf("netplay/prepare resync participants: %w", err)
		}
	case netplay.PeersConnect:
		_, err := recordstore.UpdateNetplaySessionParticipants(
			ctx,
			records.executor,
			recordstore.Update{
				Set: `state='CONNECTED',version=version+1,updated_at_ms=?`,
				Scope: recordstore.Scope{
					Where: `netplay_session_id=? AND state IN ('RUNTIME_READY','SYNCHRONIZED')`,
					Args:  []any{plan.Before.SessionID},
				},
				Values: []any{plan.Now},
			},
		)
		if err != nil {
			return fmt.Errorf("netplay/connect participants: %w", err)
		}
	default:
		return netplay.ErrRoomConflict
	}
	return nil
}

func (records sessionControlRecords) Peer(ctx context.Context, plan netplay.PeerTransitionPlan) error {
	if err := records.fence(ctx, plan.Before); err != nil {
		return err
	}
	result, err := recordstore.UpdateNetplaySessionParticipants(
		ctx,
		records.executor,
		recordstore.Update{
			Set: `state=?,disconnected_at_ms=?,lease_expires_at_ms=?,version=version+1,updated_at_ms=?`,
			Scope: recordstore.Scope{
				Where: `netplay_session_id=? AND profile_id=? AND player_no=?
AND credential_generation=? AND version=? AND state=?`,
				Args: []any{
					plan.Before.SessionID,
					plan.Peer.ProfileID,
					plan.Peer.PlayerNo,
					plan.Peer.CredentialGeneration,
					plan.Peer.Version,
					plan.Peer.State,
				},
			},
			Values: []any{plan.Target, plan.DisconnectedAtMS, plan.LeaseExpiresAtMS, plan.Now},
		},
	)
	if err := requireRoomChange(result, err, netplay.ErrRoomConflict); err != nil {
		return err
	}
	return records.events(ctx, plan.Before, plan.Events, plan.Now)
}

func (records sessionControlRecords) events(
	ctx context.Context,
	before netplay.SessionControlSnapshot,
	events []netplay.SessionEvent,
	now int64,
) error {
	for _, event := range events {
		data, err := json.Marshal(event.Data)
		if err != nil {
			return fmt.Errorf("netplay/encode session control event: %w", err)
		}
		_, err = records.executor.ExecContext(
			ctx,
			`INSERT INTO netplay_events(room_id,netplay_session_id,profile_id,player_no,event_type,data_json,created_at_ms)
VALUES(?,?,?,?,?,?,?)`,
			before.RoomID,
			before.SessionID,
			event.ActorID,
			event.PlayerNo,
			event.Type,
			string(data),
			now,
		)
		if err != nil {
			return fmt.Errorf("netplay/append session control event: %w", err)
		}
	}
	return nil
}
