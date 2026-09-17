package netplay

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/model/netplay"
	"retrom/internal/repo/dbexec"
)

type SessionControl struct{ database *sql.DB }

func NewSessionControl(database *sql.DB) *SessionControl { return &SessionControl{database} }

func (repository *SessionControl) withControl(
	ctx context.Context,
	roomID, sessionID string,
	work func(sessionControlRecords, netplay.SessionControlSnapshot) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("netplay/begin session control: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := sessionControlRecords{tx}
	before, err := records.Current(ctx, roomID, sessionID)
	if err != nil {
		return fmt.Errorf("netplay/read session control: %w", err)
	}
	if err := work(records, before); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("netplay/commit session control: %w", err)
	}
	return nil
}

func (repository *SessionControl) CommitSetSessionState(
	ctx context.Context, cmd netplay.SetSessionStateCommand,
) error {
	return repository.withControl(ctx, cmd.RoomID, cmd.SessionID,
		func(records sessionControlRecords, before netplay.SessionControlSnapshot) error {
			if before.HostID != cmd.ActorID {
				return netplay.ErrForbidden
			}
			if cmd.Target == "PAUSED_RECONNECT" && before.State != "RUNNING" ||
				cmd.Target == "RUNNING" && before.State != "PAUSED_RECONNECT" {
				return netplay.ErrRoomConflict
			}
			kind := "PAUSED"
			if cmd.Target == "RUNNING" {
				kind = "RESUMED"
			}
			event := netplay.SessionEvent{
				Type: kind,
				Data: netplay.SessionEventData{
					SchemaVersion: 1,
					FromState:     before.State,
					ToState:       cmd.Target,
				},
			}
			event.ActorID = &cmd.ActorID
			player := 1
			event.PlayerNo = &player
			return records.Session(ctx, netplay.SessionTransitionPlan{
				Before: before,
				Target: cmd.Target,
				Events: []netplay.SessionEvent{event},
				Now:    cmd.NowMS,
			})
		},
	)
}

func (repository *SessionControl) CommitResync(
	ctx context.Context, cmd netplay.ResyncCommand,
) error {
	return repository.withControl(ctx, cmd.RoomID, cmd.SessionID,
		func(records sessionControlRecords, before netplay.SessionControlSnapshot) error {
			if !netplay.ValidResyncSource(cmd.Cause, before.State) {
				return netplay.ErrRoomConflict
			}
			kind := "RESUMED"
			if cmd.Cause == netplay.ResyncHash {
				kind = "PAUSED"
			}
			event := netplay.SessionEvent{
				Type: kind,
				Data: netplay.SessionEventData{
					SchemaVersion: 1,
					FromState:     before.State,
					ToState:       "RESYNCHRONIZING",
					Reason:        string(cmd.Cause),
				},
			}
			return records.Session(ctx, netplay.SessionTransitionPlan{
				Before:          before,
				Target:          "RESYNCHRONIZING",
				IncrementResync: true,
				PeerMode:        netplay.PeersPrepareResync,
				Events:          []netplay.SessionEvent{event},
				Now:             cmd.NowMS,
			})
		},
	)
}

func (repository *SessionControl) CommitRunning(
	ctx context.Context, cmd netplay.RunningCommand,
) error {
	return repository.withControl(ctx, cmd.RoomID, cmd.SessionID,
		func(records sessionControlRecords, before netplay.SessionControlSnapshot) error {
			if before.State != "SYNCHRONIZING" && before.State != "RESYNCHRONIZING" {
				return netplay.ErrRoomConflict
			}
			events := []netplay.SessionEvent{{
				Type: "SESSION_STATE_CHANGED",
				Data: netplay.SessionEventData{
					SchemaVersion: 1,
					FromState:     before.State,
					ToState:       "RUNNING",
				},
			}}
			if before.State == "RESYNCHRONIZING" {
				events = append(events, netplay.SessionEvent{
					Type: "RESYNCED",
					Data: netplay.SessionEventData{
						SchemaVersion: 1,
						ResyncCount:   &before.ResyncCount,
					},
				})
			}
			return records.Session(ctx, netplay.SessionTransitionPlan{
				Before:   before,
				Target:   "RUNNING",
				Started:  true,
				PeerMode: netplay.PeersConnect,
				Events:   events,
				Now:      cmd.NowMS,
			})
		},
	)
}

func (repository *SessionControl) CommitDisconnected(
	ctx context.Context, cmd netplay.DisconnectedCommand,
) error {
	return repository.withControl(ctx, cmd.Identity.RoomID, cmd.Identity.SessionID,
		func(records sessionControlRecords, before netplay.SessionControlSnapshot) error {
			peer, err := netplay.ControlPeer(before, cmd.Identity)
			if err != nil {
				return fmt.Errorf("netplay/control peer disconnect: %w", err)
			}
			if peer.State != "CONNECTED" {
				return nil
			}
			lease := cmd.NowMS + cmd.LeaseMS
			if err := records.Peer(ctx, netplay.PeerTransitionPlan{
				Before:           before,
				Peer:             peer,
				Target:           "DISCONNECTED",
				DisconnectedAtMS: &cmd.NowMS,
				LeaseExpiresAtMS: &lease,
				Now:              cmd.NowMS,
			}); err != nil {
				return fmt.Errorf("netplay/write peer transition: %w", err)
			}
			if before.State != "RUNNING" {
				return nil
			}
			event := netplay.SessionEvent{
				Type: "PAUSED",
				Data: netplay.SessionEventData{
					SchemaVersion: 1,
					FromState:     "RUNNING",
					ToState:       "PAUSED_RECONNECT",
					Reason:        "PEER_DISCONNECTED",
				},
			}
			event.ActorID = &cmd.Identity.ProfileID
			event.PlayerNo = &cmd.Identity.PlayerNo
			return records.Session(ctx, netplay.SessionTransitionPlan{
				Before: before,
				Target: "PAUSED_RECONNECT",
				Events: []netplay.SessionEvent{event},
				Now:    cmd.NowMS,
			})
		},
	)
}

func (repository *SessionControl) CommitRuntimeReady(
	ctx context.Context, cmd netplay.RuntimeReadyCommand,
) (bool, error) {
	allReady := false
	err := repository.withControl(ctx, cmd.Identity.RoomID, cmd.Identity.SessionID,
		func(records sessionControlRecords, before netplay.SessionControlSnapshot) error {
			peer, err := netplay.ControlPeer(before, cmd.Identity)
			if err != nil {
				return fmt.Errorf("netplay/control peer ready: %w", err)
			}
			if peer.State == "LAUNCH_READY" {
				event := netplay.SessionEvent{
					Type: "PARTICIPANT_STATE_CHANGED",
					Data: netplay.SessionEventData{
						SchemaVersion: 1,
						FromState:     "LAUNCH_READY",
						ToState:       "RUNTIME_READY",
					},
				}
				event.ActorID = &cmd.Identity.ProfileID
				event.PlayerNo = &cmd.Identity.PlayerNo
				if err := records.Peer(ctx, netplay.PeerTransitionPlan{
					Before: before, Peer: peer, Target: "RUNTIME_READY",
					Events: []netplay.SessionEvent{event}, Now: cmd.NowMS,
				}); err != nil {
					return fmt.Errorf("netplay/write peer transition: %w", err)
				}
				peer.State = "RUNTIME_READY"
			}
			allReady = before.State == "LOADING" &&
				netplay.AllControlPeersReady(before, peer)
			if !allReady {
				return nil
			}
			event := netplay.SessionEvent{
				Type: "SESSION_STATE_CHANGED",
				Data: netplay.SessionEventData{
					SchemaVersion: 1,
					FromState:     "LOADING",
					ToState:       "SYNCHRONIZING",
				},
			}
			return records.Session(ctx, netplay.SessionTransitionPlan{
				Before: before,
				Target: "SYNCHRONIZING",
				Events: []netplay.SessionEvent{event},
				Now:    cmd.NowMS,
			})
		},
	)
	return allReady, err
}

type sessionControlRecords struct{ executor dbexec.Executor }

func (records sessionControlRecords) Current(
	ctx context.Context,
	roomID, sessionID string,
) (netplay.SessionControlSnapshot, error) {
	var result netplay.SessionControlSnapshot
	err := records.executor.QueryRowContext(ctx, `
SELECT room.id,session.id,room.host_profile_id,room.state,room.version,
session.state,session.version,session.resync_count
FROM netplay_rooms room JOIN netplay_sessions session ON session.room_id=room.id AND session.id=room.current_session_id
WHERE room.id=? AND session.id=? AND room.state IN ('STARTING','RUNNING')
AND session.state NOT IN ('FINISHED','FAILED')`, roomID, sessionID).Scan(
		&result.RoomID,
		&result.SessionID,
		&result.HostID,
		&result.RoomState,
		&result.RoomVersion,
		&result.State,
		&result.Version,
		&result.ResyncCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return netplay.SessionControlSnapshot{}, netplay.ErrSessionNotFound
	}
	if err != nil {
		return netplay.SessionControlSnapshot{}, fmt.Errorf("netplay/read controlled session: %w", err)
	}
	peers, err := records.peers(ctx, sessionID)
	if err != nil {
		return netplay.SessionControlSnapshot{}, err
	}
	result.Peers = peers
	return result, nil
}

func (records sessionControlRecords) peers(
	ctx context.Context, sessionID string,
) ([]netplay.SessionPeer, error) {
	rows, err := records.executor.QueryContext(
		ctx,
		`SELECT profile_id,player_no,credential_generation,state,version
FROM netplay_session_participants WHERE netplay_session_id=? ORDER BY player_no`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("netplay/read controlled participants: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]netplay.SessionPeer, 0, 4)
	for rows.Next() {
		var peer netplay.SessionPeer
		if err := rows.Scan(
			&peer.ProfileID,
			&peer.PlayerNo,
			&peer.CredentialGeneration,
			&peer.State,
			&peer.Version,
		); err != nil {
			return nil, fmt.Errorf("netplay/scan controlled participant: %w", err)
		}
		result = append(result, peer)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netplay/iterate controlled participants: %w", err)
	}
	return result, nil
}
