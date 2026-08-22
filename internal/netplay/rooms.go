package netplay

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"retrom/internal/cleanup"
)

type RoomMember struct {
	MemberID        string `json:"memberId"`
	ProfileID       string `json:"-"`
	PlayerNo        int    `json:"playerNo"`
	Role            string `json:"role"`
	DisplayName     string `json:"displayName"`
	AvatarRef       any    `json:"avatarRef"`
	Ready           bool   `json:"ready"`
	ConnectionState string `json:"connectionState"`
}

type RoomGame struct {
	GameID            string `json:"gameId"`
	Title             string `json:"title"`
	PlatformName      string `json:"platformName"`
	ProfileID         string `json:"profileId"`
	CoreName          string `json:"coreName"`
	EmulatorJSVersion string `json:"emulatorjsVersion"`
	MaxPlayers        int    `json:"maxPlayers"`
}

type SessionSummary struct {
	SessionID string `json:"sessionId"`
	SessionNo int    `json:"sessionNo"`
	State     string `json:"state"`
}

type RoomPermissions struct {
	Host      bool `json:"host"`
	Member    bool `json:"member"`
	CanSelect bool `json:"canSelectGame"`
	CanJoin   bool `json:"canJoin"`
	CanReady  bool `json:"canReady"`
	CanStart  bool `json:"canStart"`
	CanClose  bool `json:"canClose"`
}

type Room struct {
	RoomID         string          `json:"roomId"`
	State          string          `json:"state"`
	Version        int64           `json:"version"`
	Game           *RoomGame       `json:"game"`
	Members        []RoomMember    `json:"members"`
	CurrentSession *SessionSummary `json:"currentSession"`
	Permissions    RoomPermissions `json:"permissions"`
	SelfMemberID   *string         `json:"selfMemberId"`
	ExpiresAtMS    int64           `json:"expiresAtMs"`
	ServerNowMS    int64           `json:"serverNowMs"`
	EndedAtMS      *int64          `json:"endedAtMs"`
	EndReason      *string         `json:"endReason"`
	UpdatedAtMS    int64           `json:"-"`
}

func (service *Service) ListRooms(
	ctx context.Context,
	profileID, view string,
	afterUpdatedAtMS int64,
	afterRoomID string,
	limit int,
) ([]Room, bool, error) {
	if view == "" {
		view = "active"
	}
	if (view != "active" && view != "recent") || limit < 1 || limit > 100 {
		return nil, false, ErrRoomConflict
	}
	query, arguments := service.listRoomsQuery(profileID, view, afterUpdatedAtMS, afterRoomID, limit)
	ids, err := service.queryRoomIDs(ctx, query, arguments, limit)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(ids) > limit
	if hasMore {
		ids = ids[:limit]
	}
	result := make([]Room, 0, len(ids))
	for _, roomID := range ids {
		room, err := service.Room(ctx, roomID, profileID)
		if err != nil {
			return nil, false, err
		}
		result = append(result, room)
	}
	return result, hasMore, nil
}

func (service *Service) listRoomsQuery(
	profileID, view string,
	afterUpdatedAtMS int64,
	afterRoomID string,
	limit int,
) (string, []any) {
	terminalClause := "room.state NOT IN ('ENDED','EXPIRED')"
	if view == "recent" {
		terminalClause = "room.state IN ('ENDED','EXPIRED') AND room.ended_at_ms>=?"
	}
	query := `
SELECT DISTINCT room.id
FROM netplay_rooms room
JOIN netplay_room_members member ON member.room_id=room.id
WHERE member.profile_id=? AND ` + terminalClause
	arguments := []any{profileID}
	if view == "recent" {
		arguments = append(arguments, service.clock.Now().Add(-24*time.Hour).UnixMilli())
	}
	if afterRoomID != "" {
		query += ` AND (room.updated_at_ms < ? OR (room.updated_at_ms = ? AND room.id < ?))`
		arguments = append(arguments, afterUpdatedAtMS, afterUpdatedAtMS, afterRoomID)
	}
	query += `
ORDER BY room.updated_at_ms DESC,room.id DESC LIMIT ?
`
	arguments = append(arguments, limit+1)
	return query, arguments
}

func (service *Service) queryRoomIDs(
	ctx context.Context, query string, arguments []any, limit int,
) ([]string, error) {
	rows, err := service.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("netplay/list rooms: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	ids := make([]string, 0, limit+1)
	for rows.Next() {
		var roomID string
		if err := rows.Scan(&roomID); err != nil {
			return nil, fmt.Errorf("netplay/list rooms: %w", err)
		}
		ids = append(ids, roomID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netplay/list rooms: %w", err)
	}
	return ids, nil
}

func (service *Service) CreateRoom(ctx context.Context, profileID string) (Room, error) {
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Room{}, fmt.Errorf("netplay/create room: %w", err)
	}
	defer cleanup.Rollback(transaction)
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
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO netplay_rooms(id,host_profile_id,state,version,expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,?,'DRAFT',1,?,?,?)
`, roomID, profileID, expires, now, now); err != nil {
		if strings.Contains(err.Error(), "netplay_rooms_one_active_host") {
			return Room{}, ErrRoomConflict
		}
		return Room{}, fmt.Errorf("netplay/create room: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
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

func (service *Service) Room(ctx context.Context, roomID, viewerProfileID string) (Room, error) {
	var result Room
	var gameID, variantID, profileID, digest, sessionID, reason sql.NullString
	var maxPlayers, endedAt sql.NullInt64
	err := service.database.QueryRowContext(ctx, `
SELECT id,state,version,selected_game_id,selected_game_variant_revision_id,netplay_profile_id,
  profile_digest,max_players,current_session_id,expires_at_ms,ended_at_ms,end_reason,updated_at_ms
FROM netplay_rooms WHERE id=?
`, roomID).Scan(
		&result.RoomID, &result.State, &result.Version, &gameID, &variantID, &profileID,
		&digest, &maxPlayers, &sessionID, &result.ExpiresAtMS, &endedAt, &reason, &result.UpdatedAtMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Room{}, ErrRoomNotFound
	}
	if err != nil {
		return Room{}, fmt.Errorf("netplay/get room: %w", err)
	}
	result.ServerNowMS = service.clock.Now().UnixMilli()
	if endedAt.Valid {
		result.EndedAtMS = &endedAt.Int64
	}
	if reason.Valid {
		result.EndReason = &reason.String
	}
	if gameID.Valid {
		var game RoomGame
		game.GameID, game.ProfileID, game.MaxPlayers = gameID.String, profileID.String, int(maxPlayers.Int64)
		if err := service.database.QueryRowContext(ctx, `
SELECT metadata.title,platform.name,core.name,artifact.emulatorjs_version
FROM games game
JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
JOIN game_variant_revisions revision ON revision.id=?
JOIN core_artifacts artifact ON artifact.id=revision.core_artifact_id
JOIN cores core ON core.id=artifact.core_id
WHERE game.id=?
`, variantID.String, gameID.String).Scan(
			&game.Title, &game.PlatformName, &game.CoreName, &game.EmulatorJSVersion,
		); err != nil {
			return Room{}, fmt.Errorf("netplay/get room game: %w", err)
		}
		result.Game = &game
	}
	members, err := service.roomMembers(ctx, roomID, sessionID)
	if err != nil {
		return Room{}, err
	}
	result.Members = members
	for _, member := range members {
		if member.ProfileID == viewerProfileID {
			value := member.MemberID
			result.SelfMemberID = &value
			result.Permissions.Member = true
			result.Permissions.Host = member.Role == "HOST"
			break
		}
	}
	if sessionID.Valid {
		var session SessionSummary
		if err := service.database.QueryRowContext(ctx, `
SELECT id,session_no,state FROM netplay_sessions WHERE id=?
`, sessionID.String).Scan(&session.SessionID, &session.SessionNo, &session.State); err != nil {
			return Room{}, fmt.Errorf("netplay/get room session: %w", err)
		}
		result.CurrentSession = &session
	}
	setRoomPermissions(&result)
	return result, nil
}

func setRoomPermissions(room *Room) {
	waiting := room.State == RoomStateWaiting
	room.Permissions.CanSelect = room.Permissions.Host && (room.State == RoomStateDraft || waiting)
	room.Permissions.CanJoin = waiting && !room.Permissions.Member
	room.Permissions.CanReady = waiting && room.Permissions.Member
	room.Permissions.CanStart = waiting && room.Permissions.Host && roomReady(room.Members)
	room.Permissions.CanClose = room.Permissions.Host && room.State != "ENDED" && room.State != "EXPIRED"
}

func (service *Service) roomMembers(
	ctx context.Context, roomID string, sessionID sql.NullString,
) ([]RoomMember, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT member.id,member.profile_id,member.player_no,member.role,profile.display_name,member.ready,
  COALESCE(participant.state,'NOT_CONNECTED')
FROM netplay_room_members member
JOIN profiles profile ON profile.id=member.profile_id
LEFT JOIN netplay_session_participants participant
  ON participant.room_member_id=member.id AND participant.netplay_session_id=?
WHERE member.room_id=? AND member.left_at_ms IS NULL
ORDER BY member.player_no
`, nullableString(sessionID), roomID)
	if err != nil {
		return nil, fmt.Errorf("netplay/list members: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]RoomMember, 0, 4)
	for rows.Next() {
		var member RoomMember
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

func roomReady(members []RoomMember) bool {
	return len(members) >= 2 && !slices.ContainsFunc(members, func(member RoomMember) bool { return !member.Ready })
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
		ManifestProfile: selected.Manifest, CoreArtifactID: selected.CoreArtifactID,
		GameVariantRevisionID:  selected.VariantRevisionID,
		DependencySnapshotJSON: selected.DependencySnapshotJSON, DefaultCoreOptions: selected.DefaultCoreOptions,
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
	defer cleanup.Rollback(transaction)
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
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET state='WAITING',selected_game_id=?,selected_game_variant_revision_id=?,
netplay_profile_id=?,profile_digest=?,max_players=?,current_session_id=NULL,version=version+1,
expires_at_ms=?,updated_at_ms=? WHERE id=? AND version=?
`, gameID, selected.VariantRevisionID, profileID, digest, selected.Manifest.MaxPlayers,
		now+service.options.WaitingIdle.Milliseconds(), now, roomID, expectedVersion)
	if err != nil {
		return serviceError("select game update", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrPrecondition
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_room_members SET ready=0,version=version+1,updated_at_ms=?
WHERE room_id=? AND left_at_ms IS NULL
		`, now, roomID); err != nil {
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
			if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET state='DRAFT',selected_game_id=NULL,selected_game_variant_revision_id=NULL,
netplay_profile_id=NULL,profile_digest=NULL,max_players=NULL,version=version+1,
expires_at_ms=?,updated_at_ms=? WHERE id=? AND version=?
	`, now+service.options.DraftIdle.Milliseconds(), now, roomID, expectedVersion); err != nil {
				return serviceError("clear game", err)
			}
			if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_room_members SET ready=0,version=version+1,updated_at_ms=?
WHERE room_id=? AND left_at_ms IS NULL
`, now, roomID); err != nil {
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
	defer cleanup.Rollback(transaction)
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
	roomResult, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET version=version+1,expires_at_ms=?,updated_at_ms=? WHERE id=? AND version=?
`, now+service.options.WaitingIdle.Milliseconds(), now, roomID, expectedVersion)
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
		if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_room_members SET player_no=?,ready=0,left_at_ms=NULL,leave_reason=NULL,
version=version+1,updated_at_ms=? WHERE id=?
		`, playerNo, now, existingID.String); err != nil {
			return "", serviceError("change seat", err)
		}
	} else {
		eventType = "MEMBER_JOINED"
		if _, err := transaction.ExecContext(ctx, `
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
	defer cleanup.Rollback(transaction)
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
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_room_members SET ready=?,version=version+1,updated_at_ms=?
WHERE room_id=? AND profile_id=? AND left_at_ms IS NULL
`, readyValue, now, roomID, profileID)
	if err != nil {
		return Room{}, serviceError("set ready member", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return Room{}, ErrForbidden
	}
	roomResult, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET version=version+1,expires_at_ms=?,updated_at_ms=? WHERE id=? AND version=?
`, now+service.options.WaitingIdle.Milliseconds(), now, roomID, expectedVersion)
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
