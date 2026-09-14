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

func (repository *RoomQueries) Snapshot(ctx context.Context, roomID string) (netplay.Room, error) {
	transaction, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return netplay.Room{}, fmt.Errorf("netplay/read room: %w", err)
	}
	defer dbexec.Rollback(transaction)
	room, err := loadRoomSnapshot(ctx, transaction, roomID)
	if err != nil {
		return netplay.Room{}, err
	}
	if err := transaction.Commit(); err != nil {
		return netplay.Room{}, fmt.Errorf("netplay/read room commit: %w", err)
	}
	return room, nil
}

func loadRoomSnapshot(ctx context.Context, transaction dbexec.Executor, roomID string) (netplay.Room, error) {
	var result netplay.Room
	var gameID, variantID, profileID, digest, sessionID, reason sql.NullString
	var maxPlayers, endedAt sql.NullInt64
	err := transaction.QueryRowContext(ctx, `
SELECT id,state,version,selected_game_id,selected_game_variant_id,netplay_profile_id,
  profile_digest,max_players,current_session_id,expires_at_ms,ended_at_ms,end_reason,updated_at_ms
FROM netplay_rooms WHERE id=?
`, roomID).Scan(
		&result.RoomID, &result.State, &result.Version, &gameID, &variantID, &profileID,
		&digest, &maxPlayers, &sessionID, &result.ExpiresAtMS, &endedAt, &reason, &result.UpdatedAtMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return netplay.Room{}, netplay.ErrRoomNotFound
	}
	if err != nil {
		return netplay.Room{}, fmt.Errorf("netplay/get room: %w", err)
	}
	if endedAt.Valid {
		result.EndedAtMS = &endedAt.Int64
	}
	if reason.Valid {
		result.EndReason = &reason.String
	}
	if gameID.Valid {
		var game netplay.RoomGame
		game.GameID, game.ProfileID, game.MaxPlayers = gameID.String, profileID.String, int(maxPlayers.Int64)
		if err := transaction.QueryRowContext(ctx, `
SELECT game.title,game.status,platform.name,core.name,variant.provider_id,variant.target_id
FROM games game
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
JOIN game_variants variant ON variant.id=? AND variant.game_id=game.id
JOIN cores core ON core.id=variant.core_id
WHERE game.id=?
`, variantID.String, gameID.String).Scan(
			&game.Title, &game.Status, &game.PlatformName, &game.CoreName, &game.ProviderID, &game.TargetID,
		); err != nil {
			return netplay.Room{}, fmt.Errorf("netplay/get room game: %w", err)
		}
		result.Game = &game
	}
	members, err := loadRoomMembers(ctx, transaction, roomID, sessionID)
	if err != nil {
		return netplay.Room{}, err
	}
	result.Members = members

	if sessionID.Valid {
		var session netplay.SessionSummary
		if err := transaction.QueryRowContext(ctx, `
SELECT id,session_no,state FROM netplay_sessions WHERE id=?
`, sessionID.String).Scan(&session.SessionID, &session.SessionNo, &session.State); err != nil {
			return netplay.Room{}, fmt.Errorf("netplay/get room session: %w", err)
		}
		result.CurrentSession = &session
	}
	return result, nil
}

func loadRoomMembers(
	ctx context.Context, transaction dbexec.Executor, roomID string, sessionID sql.NullString,
) ([]netplay.RoomMember, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT member.id,member.profile_id,member.player_no,member.role,profile.display_name,member.ready,
  COALESCE(participant.state,'NOT_CONNECTED')
FROM netplay_room_members member
JOIN profiles profile ON profile.id=member.profile_id
LEFT JOIN netplay_session_participants participant
  ON participant.room_member_id=member.id AND participant.netplay_session_id=?
WHERE member.room_id=? AND member.left_at_ms IS NULL
ORDER BY member.player_no
`, sessionID, roomID)
	if err != nil {
		return nil, fmt.Errorf("netplay/list members: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]netplay.RoomMember, 0, 4)
	for rows.Next() {
		var member netplay.RoomMember
		var ready int
		if err := rows.Scan(
			&member.MemberID, &member.ProfileID, &member.PlayerNo, &member.Role,
			&member.DisplayName, &ready, &member.ConnectionState,
		); err != nil {
			return nil, fmt.Errorf("netplay/scan member: %w", err)
		}
		member.Ready = ready == 1
		result = append(result, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netplay/list members: %w", err)
	}
	return result, nil
}
