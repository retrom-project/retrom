package netplay

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/persistence/dbexec"
	"retrom/internal/service/netplay"
)

type SessionControl struct{ database *sql.DB }

func NewSessionControl(database *sql.DB) *SessionControl { return &SessionControl{database} }
func (repository *SessionControl) WithControl(ctx context.Context, work func(netplay.SessionControlScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("netplay/begin session control: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := sessionControlRecords{tx}
	if err := work(netplay.SessionControlScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("netplay/commit session control: %w", err)
	}
	return nil
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

func (records sessionControlRecords) peers(ctx context.Context, sessionID string) ([]netplay.SessionPeer, error) {
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
