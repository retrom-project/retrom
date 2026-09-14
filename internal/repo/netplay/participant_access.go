package netplay

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/model/netplay"
	"retrom/internal/repo/dbexec"
)

type ParticipantAccess struct{ executor dbexec.Executor }

func NewParticipantAccess(executor dbexec.Executor) *ParticipantAccess {
	return &ParticipantAccess{executor}
}

func (repository *ParticipantAccess) Socket(
	ctx context.Context,
	roomID, profileID string,
) (netplay.SocketAccessRecord, error) {
	var result netplay.SocketAccessRecord
	participant := &result.Participant
	err := repository.executor.QueryRowContext(ctx, `
SELECT room.id,session.id,participant.profile_id,participant.player_no,
participant.credential_generation,session.profile_digest,session.provider_id,session.target_id,session.bundle_sha256,
room.version,session.version,session.state,session.occupied_seat_mask,session.player_count,
participant.credential_sha256,launch.state
FROM netplay_rooms room
JOIN netplay_sessions session ON session.id=room.current_session_id AND session.room_id=room.id
JOIN netplay_session_participants participant ON participant.netplay_session_id=session.id
JOIN launch_sessions launch ON launch.id=participant.launch_session_id
WHERE room.id=? AND participant.profile_id=? AND room.state IN ('STARTING','RUNNING')
AND session.state NOT IN ('FINISHED','FAILED')`, roomID, profileID).Scan(
		&participant.RoomID,
		&participant.SessionID,
		&participant.ProfileID,
		&participant.PlayerNo,
		&participant.CredentialGeneration,
		&participant.ProfileDigest,
		&participant.ProviderID,
		&participant.TargetID,
		&participant.BundleSHA256,
		&participant.RoomVersion,
		&participant.SessionVersion,
		&participant.SessionState,
		&participant.OccupiedSeatMask,
		&participant.PlayerCount,
		&result.CredentialHash,
		&result.LaunchState,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return netplay.SocketAccessRecord{}, netplay.ErrForbidden
	}
	if err != nil {
		return netplay.SocketAccessRecord{}, fmt.Errorf("netplay/read socket credential: %w", err)
	}
	return result, nil
}

func (repository *ParticipantAccess) Credential(
	ctx context.Context,
	sessionID, profileID string,
) (netplay.ParticipantCredentialRecord, error) {
	var result netplay.ParticipantCredentialRecord
	err := repository.executor.QueryRowContext(ctx, `
SELECT credential_generation,credential_sha256 FROM netplay_session_participants
WHERE netplay_session_id=? AND profile_id=? AND launch_session_id IS NOT NULL`, sessionID, profileID).Scan(
		&result.Generation,
		&result.CredentialHash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return netplay.ParticipantCredentialRecord{}, netplay.ErrForbidden
	}
	if err != nil {
		return netplay.ParticipantCredentialRecord{}, fmt.Errorf("netplay/read participant credential: %w", err)
	}
	return result, nil
}
