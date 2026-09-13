package netplay

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/dbexec"
	validation "retrom/internal/persistence/corevalidation"
	"retrom/internal/service/netplay"
)

type RoomControl struct{ database *sql.DB }

func NewRoomControl(database *sql.DB) *RoomControl { return &RoomControl{database: database} }
func (repository *RoomControl) WithWrite(ctx context.Context, work func(netplay.RoomControlScope) error) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("netplay/begin room control: %w", err)
	}
	defer dbexec.Rollback(transaction)
	records := roomControlRecords{transaction}
	scope := netplay.RoomControlScope{
		Read:        records,
		Write:       records,
		Eligibility: NewEligibility(transaction),
		BIOS:        validation.New(transaction),
	}
	if err := work(scope); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("netplay/commit room control: %w", err)
	}
	return nil
}

type roomControlRecords struct{ executor dbexec.Executor }

func (records roomControlRecords) Snapshot(ctx context.Context, id string) (netplay.Room, error) {
	return loadRoomSnapshot(ctx, records.executor, id)
}

func (records roomControlRecords) Current(
	ctx context.Context,
	roomID, actorID string,
) (netplay.RoomControlSnapshot, error) {
	var result netplay.RoomControlSnapshot
	var gameID, variantID, profileID, digest sql.NullString
	var maxPlayers sql.NullInt64
	err := records.executor.QueryRowContext(ctx, `
SELECT id,host_profile_id,state,version,selected_game_id,selected_game_variant_id,
netplay_profile_id,profile_digest,max_players FROM netplay_rooms WHERE id=?
`, roomID).Scan(
		&result.RoomID,
		&result.HostID,
		&result.State,
		&result.Version,
		&gameID,
		&variantID,
		&profileID,
		&digest,
		&maxPlayers,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return netplay.RoomControlSnapshot{}, netplay.ErrRoomNotFound
	}
	if err != nil {
		return netplay.RoomControlSnapshot{}, fmt.Errorf("netplay/read room control: %w", err)
	}
	if gameID.Valid {
		result.Selection = &netplay.RoomSelection{
			GameID:     gameID.String,
			VariantID:  variantID.String,
			ProfileID:  profileID.String,
			Digest:     digest.String,
			MaxPlayers: int(maxPlayers.Int64),
		}
	}
	if err := records.controlMembers(ctx, &result, actorID); err != nil {
		return netplay.RoomControlSnapshot{}, err
	}
	return result, nil
}

func (records roomControlRecords) controlMembers(
	ctx context.Context,
	result *netplay.RoomControlSnapshot,
	actorID string,
) error {
	rows, err := records.executor.QueryContext(ctx, `
SELECT id,profile_id,role,player_no,ready,version,left_at_ms FROM netplay_room_members
WHERE room_id=? AND (left_at_ms IS NULL OR profile_id=?) ORDER BY player_no,id
`, result.RoomID, actorID)
	if err != nil {
		return fmt.Errorf("netplay/read control members: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result.Occupants = make([]netplay.SeatMember, 0, 4)
	for rows.Next() {
		var member netplay.SeatMember
		if err := rows.Scan(
			&member.ID,
			&member.ProfileID,
			&member.Role,
			&member.PlayerNo,
			&member.Ready,
			&member.Version,
			&member.LeftAtMS,
		); err != nil {
			return fmt.Errorf("netplay/scan control member: %w", err)
		}
		if member.ProfileID == actorID {
			currentMember := member
			result.Member = &currentMember
		}
		if member.LeftAtMS == nil {
			result.Occupants = append(result.Occupants, member)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("netplay/iterate control members: %w", err)
	}
	return nil
}
