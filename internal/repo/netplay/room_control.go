package netplay

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/model/netplay"
	"retrom/internal/repo/dbexec"
)

type RoomControl struct{ database *sql.DB }

func NewRoomControl(database *sql.DB) *RoomControl { return &RoomControl{database: database} }

func (repository *RoomControl) LoadControlSnapshot(
	ctx context.Context, roomID, actorID string,
) (netplay.RoomControlSnapshot, error) {
	records := roomControlRecords{repository.database}
	return records.Current(ctx, roomID, actorID)
}

// roomMutationGuard opens a transaction, reads the room snapshot, and
// validates version, host-only and allowed-state guards.
func (repository *RoomControl) roomMutationGuard(
	ctx context.Context,
	roomID, actorID string,
	version int64,
	hostOnly bool,
	allowedStates []string,
) (*sql.Tx, roomControlRecords, netplay.RoomControlSnapshot, error) {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, roomControlRecords{}, netplay.RoomControlSnapshot{},
			fmt.Errorf("netplay/begin room control: %w", err)
	}
	records := roomControlRecords{transaction}
	before, err := records.Current(ctx, roomID, actorID)
	if err != nil {
		dbexec.Rollback(transaction)
		return nil, records, before,
			fmt.Errorf("netplay/read room control: %w", err)
	}
	if hostOnly && before.HostID != actorID {
		dbexec.Rollback(transaction)
		return nil, records, before, netplay.ErrForbidden
	}
	if before.Version != version {
		dbexec.Rollback(transaction)
		return nil, records, before, netplay.ErrPrecondition
	}
	found := false
	for _, s := range allowedStates {
		if s == before.State {
			found = true
			break
		}
	}
	if !found {
		dbexec.Rollback(transaction)
		return nil, records, before, netplay.ErrRoomConflict
	}
	return transaction, records, before, nil
}

func (repository *RoomControl) commitRoomResult(
	ctx context.Context,
	transaction *sql.Tx,
	records roomControlRecords,
	roomID string,
) (netplay.Room, error) {
	result, err := records.Snapshot(ctx, roomID)
	if err != nil {
		return netplay.Room{}, fmt.Errorf("netplay/read updated room: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return netplay.Room{}, fmt.Errorf("netplay/commit room control: %w", err)
	}
	return result, nil
}

func (repository *RoomControl) CommitSelectGame(
	ctx context.Context, cmd netplay.SelectGameCommand,
) (netplay.Room, error) {
	transaction, records, before, err := repository.roomMutationGuard(
		ctx, cmd.RoomID, cmd.ActorID, cmd.Version, true,
		[]string{netplay.RoomStateDraft, netplay.RoomStateWaiting},
	)
	if err != nil {
		return netplay.Room{}, err
	}
	defer dbexec.Rollback(transaction)

	for _, member := range before.Occupants {
		if member.PlayerNo > cmd.Selection.MaxPlayers {
			return netplay.Room{}, netplay.ErrInvalidSeat
		}
	}
	data, err := json.Marshal(struct {
		SchemaVersion int `json:"schemaVersion"`
		PlayerCount   int `json:"playerCount"`
	}{1, cmd.Selection.MaxPlayers})
	if err != nil {
		return netplay.Room{}, fmt.Errorf("netplay/selection event: %w", err)
	}
	player := 1
	if err := records.Select(ctx, netplay.RoomSelectionPlan{
		Before:    before,
		Selection: cmd.Selection,
		Evidence: netplay.RoomControlEvidence{
			ActorID:     cmd.ActorID,
			Type:        "GAME_SELECTED",
			PlayerNo:    &player,
			Data:        data,
			Now:         cmd.NowMS,
			ExpiresAtMS: cmd.NowMS + cmd.IdleMS,
		},
	}); err != nil {
		return netplay.Room{}, fmt.Errorf("netplay/select game: %w", err)
	}
	return repository.commitRoomResult(ctx, transaction, records, cmd.RoomID)
}

func (repository *RoomControl) CommitClearGame(
	ctx context.Context, cmd netplay.ClearGameCommand,
) (netplay.Room, error) {
	transaction, records, before, err := repository.roomMutationGuard(
		ctx, cmd.RoomID, cmd.ActorID, cmd.Version, true,
		[]string{netplay.RoomStateWaiting},
	)
	if err != nil {
		return netplay.Room{}, err
	}
	defer dbexec.Rollback(transaction)

	player := 1
	if err := records.Clear(ctx, netplay.RoomClearPlan{
		Before: before,
		Evidence: netplay.RoomControlEvidence{
			ActorID:     cmd.ActorID,
			Type:        "GAME_CLEARED",
			PlayerNo:    &player,
			Data:        []byte(`{"schemaVersion":1}`),
			Now:         cmd.NowMS,
			ExpiresAtMS: cmd.NowMS + cmd.IdleMS,
		},
	}); err != nil {
		return netplay.Room{}, fmt.Errorf("netplay/clear game: %w", err)
	}
	return repository.commitRoomResult(ctx, transaction, records, cmd.RoomID)
}

func (repository *RoomControl) CommitSetSeat(
	ctx context.Context, cmd netplay.SetSeatCommand,
) (netplay.Room, error) {
	transaction, records, before, err := repository.roomMutationGuard(
		ctx, cmd.RoomID, cmd.ActorID, cmd.Version, false,
		[]string{netplay.RoomStateWaiting},
	)
	if err != nil {
		return netplay.Room{}, err
	}
	defer dbexec.Rollback(transaction)

	if err := netplay.ValidateSeat(before, cmd.PlayerNo, cmd.ActorID); err != nil {
		return netplay.Room{}, err
	}
	plan, err := netplay.BuildSeatPlan(
		before, cmd.ActorID, cmd.PlayerNo,
		cmd.NewMemberID, cmd.NowMS, cmd.IdleMS,
	)
	if err != nil {
		return netplay.Room{}, err
	}
	if err := records.Seat(ctx, plan); err != nil {
		return netplay.Room{}, fmt.Errorf("netplay/set seat: %w", err)
	}
	return repository.commitRoomResult(ctx, transaction, records, cmd.RoomID)
}

func (repository *RoomControl) CommitSetReady(
	ctx context.Context, cmd netplay.SetReadyCommand,
) (netplay.Room, error) {
	transaction, records, before, err := repository.roomMutationGuard(
		ctx, cmd.RoomID, cmd.ActorID, cmd.Version, false,
		[]string{netplay.RoomStateWaiting},
	)
	if err != nil {
		return netplay.Room{}, err
	}
	defer dbexec.Rollback(transaction)

	if before.Member == nil || before.Member.LeftAtMS != nil {
		return netplay.Room{}, netplay.ErrForbidden
	}
	data, err := json.Marshal(struct {
		SchemaVersion int  `json:"schemaVersion"`
		Ready         bool `json:"ready"`
	}{1, cmd.Ready})
	if err != nil {
		return netplay.Room{}, fmt.Errorf("netplay/ready event: %w", err)
	}
	if err := records.Ready(ctx, netplay.RoomReadyPlan{
		Before: before,
		Ready:  cmd.Ready,
		Evidence: netplay.RoomControlEvidence{
			ActorID:     cmd.ActorID,
			Type:        "READY_CHANGED",
			Data:        data,
			Now:         cmd.NowMS,
			ExpiresAtMS: cmd.NowMS + cmd.IdleMS,
		},
	}); err != nil {
		return netplay.Room{}, fmt.Errorf("netplay/set ready: %w", err)
	}
	return repository.commitRoomResult(ctx, transaction, records, cmd.RoomID)
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
