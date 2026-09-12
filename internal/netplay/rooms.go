package netplay

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	application "retrom/internal/service/netplay"

	"retrom/internal/dbexec"

	"retrom/internal/persistence/recordstore"
)

func (service *Service) CreateRoom(ctx context.Context, profileID string) (Room, error) {
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Room{}, fmt.Errorf("netplay/create room: %w", err)
	}
	defer dbexec.Rollback(transaction)
	var active int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM netplay_rooms WHERE state IN ('DRAFT','WAITING','STARTING','RUNNING')
`).Scan(&active); err != nil {
		return Room{}, fmt.Errorf("netplay/create room: %w", err)
	}
	if active >= service.options.MaxActiveRooms {
		return Room{}, ErrCapacity
	}
	roomID, memberID := newV7(), newV7()
	if roomID == "" || memberID == "" {
		return Room{}, errUUIDUnavailable
	}
	expires := now + service.options.DraftIdle.Milliseconds()
	if _, err := recordstore.CreateNetplayRooms(ctx, transaction, `
INSERT INTO netplay_rooms(id,host_profile_id,state,version,expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,?,'DRAFT',1,?,?,?)
`, roomID, profileID, expires, now, now); err != nil {
		if strings.Contains(err.Error(), "netplay_rooms_one_active_host") {
			return Room{}, ErrRoomConflict
		}
		return Room{}, fmt.Errorf("netplay/create room: %w", err)
	}
	if _, err := recordstore.CreateNetplayRoomMembers(ctx, transaction, `
INSERT INTO netplay_room_members(id,room_id,profile_id,role,player_no,ready,version,joined_at_ms,updated_at_ms)
VALUES(?,?,?,'HOST',1,0,1,?,?)
`, memberID, roomID, profileID, now, now); err != nil {
		return Room{}, fmt.Errorf("netplay/create host: %w", err)
	}
	if err := appendEvent(ctx, transaction, roomID, nil, &profileID, intPointer(1), "ROOM_CREATED", nil, now); err != nil {
		return Room{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Room{}, fmt.Errorf("netplay/create room: %w", err)
	}
	return service.Room(ctx, roomID, profileID)
}

func (service *Service) SelectGame(
	ctx context.Context, roomID, actorProfileID, gameID, profileID string, expectedVersion int64,
) (Room, error) {
	profiles, err := service.eligibleProfiles(ctx, gameID)
	if err != nil {
		return Room{}, fmt.Errorf("netplay/select game profiles: %w", err)
	}
	selected := findEligibleProfile(profiles, profileID)
	if selected == nil {
		return Room{}, ErrInvalidProfile
	}
	canonical, digest, err := service.registry.CanonicalProfile(CanonicalProfileInput{
		ManifestProfile: selected.Manifest, BundleSHA256: selected.BundleSHA256,
		SourceManifestDigest:   selected.SourceManifestDigest,
		DependencySnapshotJSON: selected.DependencySnapshotJSON,
	})
	if err != nil || len(canonical) == 0 {
		return Room{}, ErrInvalidProfile
	}
	if err := service.commitSelectedGame(
		ctx, roomID, actorProfileID, gameID, profileID, digest, selected, expectedVersion,
	); err != nil {
		return Room{}, err
	}
	return service.Room(ctx, roomID, actorProfileID)
}

func (service *Service) commitSelectedGame(
	ctx context.Context,
	roomID, actorProfileID, gameID, profileID, digest string,
	selected *eligibleProfile,
	expectedVersion int64,
) error {
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("select game transaction", err)
	}
	defer dbexec.Rollback(transaction)
	var state string
	var host string
	var version int64
	if err := transaction.QueryRowContext(
		ctx, `SELECT state,host_profile_id,version FROM netplay_rooms WHERE id=?`, roomID,
	).Scan(&state, &host, &version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRoomNotFound
		}
		return serviceError("select game room", err)
	}
	if host != actorProfileID {
		return ErrForbidden
	}
	if version != expectedVersion {
		return ErrPrecondition
	}
	if state != RoomStateDraft && state != RoomStateWaiting {
		return ErrRoomConflict
	}
	var invalidSeat int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM netplay_room_members WHERE room_id=? AND left_at_ms IS NULL AND player_no>?
		`, roomID, selected.Manifest.MaxPlayers).Scan(&invalidSeat); err != nil {
		return serviceError("select game seats", err)
	}
	if invalidSeat > 0 {
		return ErrInvalidSeat
	}
	result, err := recordstore.UpdateNetplayRooms(ctx, transaction, recordstore.Update{
		Set: `
state='WAITING',selected_game_id=?,selected_game_variant_id=?,
netplay_profile_id=?,profile_digest=?,max_players=?,current_session_id=NULL,version=version+1,
expires_at_ms=?,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=? AND version=?`,
			Args:  []any{roomID, expectedVersion},
		},
		Values: []any{
			gameID,
			selected.VariantID,
			profileID,
			digest,
			selected.Manifest.MaxPlayers,
			now + service.options.WaitingIdle.Milliseconds(),
			now,
		},
	})
	if err != nil {
		return serviceError("select game update", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrPrecondition
	}
	if _, err := recordstore.UpdateNetplayRoomMembers(ctx, transaction, recordstore.Update{
		Set: `ready=0,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `room_id=? AND left_at_ms IS NULL`,
			Args:  []any{roomID},
		},
		Values: []any{now},
	}); err != nil {
		return serviceError("select game clear ready", err)
	}
	data := map[string]any{"schemaVersion": 1, "playerCount": selected.Manifest.MaxPlayers}
	if err := appendEvent(
		ctx, transaction, roomID, nil, &actorProfileID, intPointer(1), "GAME_SELECTED", data, now,
	); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return serviceError("select game commit", err)
	}
	return nil
}

func findEligibleProfile(profiles []eligibleProfile, profileID string) *eligibleProfile {
	for index := range profiles {
		if profiles[index].Manifest.ID == profileID {
			return &profiles[index]
		}
	}
	return nil
}

func (service *Service) ClearGame(
	ctx context.Context,
	roomID, actorProfileID string,
	expectedVersion int64,
) (Room, error) {
	return service.mutateHostRoom(
		ctx, roomID, actorProfileID, expectedVersion, []string{RoomStateWaiting},
		func(transaction *sql.Tx, now int64) error {
			if _, err := recordstore.UpdateNetplayRooms(ctx, transaction, recordstore.Update{
				Set: `
state='DRAFT',selected_game_id=NULL,selected_game_variant_id=NULL,
netplay_profile_id=NULL,profile_digest=NULL,max_players=NULL,version=version+1,
expires_at_ms=?,updated_at_ms=?
`,
				Scope: recordstore.Scope{
					Where: `id=? AND version=?`,
					Args:  []any{roomID, expectedVersion},
				},
				Values: []any{now + service.options.DraftIdle.Milliseconds(), now},
			}); err != nil {
				return serviceError("clear game", err)
			}
			if _, err := recordstore.UpdateNetplayRoomMembers(ctx, transaction, recordstore.Update{
				Set: `ready=0,version=version+1,updated_at_ms=?`,
				Scope: recordstore.Scope{
					Where: `room_id=? AND left_at_ms IS NULL`,
					Args:  []any{roomID},
				},
				Values: []any{now},
			}); err != nil {
				return serviceError("clear game ready state", err)
			}
			return appendEvent(ctx, transaction, roomID, nil, &actorProfileID, intPointer(1), "GAME_CLEARED", nil, now)
		},
	)
}

func (service *Service) SetSeat(
	ctx context.Context,
	roomID, profileID string,
	playerNo int,
	expectedVersion int64,
) (Room, error) {
	if playerNo < 2 || playerNo > 4 {
		return Room{}, ErrInvalidSeat
	}
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Room{}, serviceError("set seat transaction", err)
	}
	defer dbexec.Rollback(transaction)
	existingID, existingPlayer, err := validateSeatMutation(
		ctx, transaction, roomID, profileID, playerNo, expectedVersion,
	)
	if err != nil {
		return Room{}, err
	}
	eventType, err := persistSeatMember(
		ctx, transaction, roomID, profileID, playerNo, existingID, now,
	)
	if err != nil {
		return Room{}, err
	}
	roomResult, err := recordstore.UpdateNetplayRooms(ctx, transaction, recordstore.Update{
		Set: `version=version+1,expires_at_ms=?,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND version=?`,
			Args:  []any{roomID, expectedVersion},
		},
		Values: []any{now + service.options.WaitingIdle.Milliseconds(), now},
	})
	if err != nil {
		return Room{}, serviceError("set seat room version", err)
	}
	if affected, _ := roomResult.RowsAffected(); affected != 1 {
		return Room{}, ErrPrecondition
	}
	data := map[string]any{"schemaVersion": 1, "toPlayerNo": playerNo}
	if existingPlayer.Valid {
		data["fromPlayerNo"] = existingPlayer.Int64
	}
	if err := appendEvent(ctx, transaction, roomID, nil, &profileID, &playerNo, eventType, data, now); err != nil {
		return Room{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Room{}, serviceError("set seat commit", err)
	}
	return service.Room(ctx, roomID, profileID)
}

func validateSeatMutation(
	ctx context.Context,
	transaction *sql.Tx,
	roomID, profileID string,
	playerNo int,
	expectedVersion int64,
) (sql.NullString, sql.NullInt64, error) {
	var state string
	var version, maxPlayers int64
	if err := transaction.QueryRowContext(
		ctx, `SELECT state,version,max_players FROM netplay_rooms WHERE id=?`, roomID,
	).Scan(&state, &version, &maxPlayers); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sql.NullString{}, sql.NullInt64{}, ErrRoomNotFound
		}
		return sql.NullString{}, sql.NullInt64{}, serviceError("set seat room", err)
	}
	if version != expectedVersion {
		return sql.NullString{}, sql.NullInt64{}, ErrPrecondition
	}
	if state != RoomStateWaiting {
		return sql.NullString{}, sql.NullInt64{}, ErrRoomConflict
	}
	if int64(playerNo) > maxPlayers {
		return sql.NullString{}, sql.NullInt64{}, ErrInvalidSeat
	}
	var existingID sql.NullString
	var existingPlayer sql.NullInt64
	var ready sql.NullInt64
	var role sql.NullString
	err := transaction.QueryRowContext(ctx, `
SELECT id,player_no,ready,role FROM netplay_room_members WHERE room_id=? AND profile_id=?
	`, roomID, profileID).Scan(&existingID, &existingPlayer, &ready, &role)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return sql.NullString{}, sql.NullInt64{}, serviceError("set seat member", err)
	}
	if err := validateSeatMember(role, ready); err != nil {
		return sql.NullString{}, sql.NullInt64{}, err
	}
	var occupied int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM netplay_room_members WHERE room_id=? AND player_no=? AND left_at_ms IS NULL
  AND profile_id!=?
		`, roomID, playerNo, profileID).Scan(&occupied); err != nil {
		return sql.NullString{}, sql.NullInt64{}, serviceError("set seat occupancy", err)
	}
	if occupied > 0 {
		return sql.NullString{}, sql.NullInt64{}, ErrSeatTaken
	}
	return existingID, existingPlayer, nil
}

func validateSeatMember(role sql.NullString, ready sql.NullInt64) error {
	if ready.Valid && ready.Int64 == 1 {
		return ErrRoomConflict
	}
	if role.Valid && role.String == "HOST" {
		return ErrForbidden
	}
	return nil
}

func persistSeatMember(
	ctx context.Context,
	transaction *sql.Tx,
	roomID, profileID string,
	playerNo int,
	existingID sql.NullString,
	now int64,
) (string, error) {
	eventType := "SEAT_CHANGED"
	if existingID.Valid {
		if _, err := recordstore.UpdateNetplayRoomMembers(ctx, transaction, recordstore.Update{
			Set: `
player_no=?,ready=0,left_at_ms=NULL,leave_reason=NULL,
version=version+1,updated_at_ms=?
`,
			Scope: recordstore.Scope{
				Where: `id=?`,
				Args:  []any{existingID.String},
			},
			Values: []any{playerNo, now},
		}); err != nil {
			return "", serviceError("change seat", err)
		}
	} else {
		eventType = "MEMBER_JOINED"
		if _, err := recordstore.CreateNetplayRoomMembers(ctx, transaction, `
INSERT INTO netplay_room_members(id,room_id,profile_id,role,player_no,ready,version,joined_at_ms,updated_at_ms)
VALUES(?,?,?,'GUEST',?,0,1,?,?)
		`, newV7(), roomID, profileID, playerNo, now, now); err != nil {
			return "", serviceError("claim seat", err)
		}
	}
	return eventType, nil
}

func (service *Service) SetReady(
	ctx context.Context,
	roomID, profileID string,
	ready bool,
	expectedVersion int64,
) (Room, error) {
	if err := service.validateReadySelection(ctx, roomID, ready, expectedVersion); err != nil {
		return Room{}, err
	}
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Room{}, serviceError("set ready transaction", err)
	}
	defer dbexec.Rollback(transaction)
	var state string
	var version int64
	if err := transaction.QueryRowContext(ctx, `
SELECT state,version FROM netplay_rooms WHERE id=?
`, roomID).Scan(&state, &version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Room{}, ErrRoomNotFound
		}
		return Room{}, serviceError("set ready room", err)
	}
	if version != expectedVersion {
		return Room{}, ErrPrecondition
	}
	if state != RoomStateWaiting {
		return Room{}, ErrRoomConflict
	}
	readyValue := boolInt(ready)
	result, err := recordstore.UpdateNetplayRoomMembers(ctx, transaction, recordstore.Update{
		Set: `ready=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `room_id=? AND profile_id=? AND left_at_ms IS NULL`,
			Args:  []any{roomID, profileID},
		},
		Values: []any{readyValue, now},
	})
	if err != nil {
		return Room{}, serviceError("set ready member", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return Room{}, ErrForbidden
	}
	roomResult, err := recordstore.UpdateNetplayRooms(ctx, transaction, recordstore.Update{
		Set: `version=version+1,expires_at_ms=?,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND version=?`,
			Args:  []any{roomID, expectedVersion},
		},
		Values: []any{now + service.options.WaitingIdle.Milliseconds(), now},
	})
	if err != nil {
		return Room{}, serviceError("set ready room version", err)
	}
	if affected, _ := roomResult.RowsAffected(); affected != 1 {
		return Room{}, ErrPrecondition
	}
	data := map[string]any{"schemaVersion": 1, "ready": ready}
	if err := appendEvent(ctx, transaction, roomID, nil, &profileID, nil, "READY_CHANGED", data, now); err != nil {
		return Room{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Room{}, serviceError("set ready commit", err)
	}
	return service.Room(ctx, roomID, profileID)
}

func (service *Service) validateReadySelection(
	ctx context.Context, roomID string, ready bool, expectedVersion int64,
) error {
	if !ready {
		return nil
	}
	var state, gameID, profileID string
	var version int64
	if err := service.database.QueryRowContext(ctx, `
SELECT state,version,selected_game_id,netplay_profile_id FROM netplay_rooms WHERE id=?
`, roomID).Scan(&state, &version, &gameID, &profileID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRoomNotFound
		}
		return serviceError("prevalidate ready room", err)
	}
	if version != expectedVersion {
		return ErrPrecondition
	}
	if state != RoomStateWaiting {
		return ErrRoomConflict
	}
	profiles, err := service.eligibleProfiles(ctx, gameID)
	if err != nil {
		return fmt.Errorf("netplay/set ready profiles: %w", err)
	}
	if findEligibleProfile(profiles, profileID) == nil {
		return ErrProfileStale
	}
	return nil
}

type (
	RoomMember      = application.RoomMember
	RoomGame        = application.RoomGame
	SessionSummary  = application.SessionSummary
	RoomPermissions = application.RoomPermissions
	Room            = application.Room
)
