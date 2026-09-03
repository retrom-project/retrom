package netplay

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"retrom/internal/cleanup"
)

func (service *Service) Start(ctx context.Context, roomID, hostProfileID string, expectedVersion int64) (Room, error) {
	prevalidated, err := loadStartRoom(ctx, service.database, roomID, hostProfileID, expectedVersion)
	if err != nil {
		return Room{}, err
	}
	locked, canonical, digest, err := service.lockedStartProfile(ctx, prevalidated)
	if err != nil {
		return Room{}, err
	}
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Room{}, serviceError("start transaction", err)
	}
	defer cleanup.Rollback(transaction)
	state, err := loadStartRoom(ctx, transaction, roomID, hostProfileID, expectedVersion)
	if err != nil {
		return Room{}, err
	}
	if state != prevalidated {
		return Room{}, ErrPrecondition
	}
	members, mask, err := loadLockedMembers(ctx, transaction, roomID, state.maxPlayers)
	if err != nil {
		return Room{}, err
	}
	sessionID, err := createNetplaySession(
		ctx, transaction, roomID, state, locked, canonical, digest, members, mask, now,
	)
	if err != nil {
		return Room{}, err
	}
	roomResult, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET state='STARTING',current_session_id=?,version=version+1,updated_at_ms=?
WHERE id=? AND version=?
`, sessionID, now, roomID, expectedVersion)
	if err != nil {
		return Room{}, serviceError("start room state", err)
	}
	if affected, _ := roomResult.RowsAffected(); affected != 1 {
		return Room{}, ErrPrecondition
	}
	data := map[string]any{
		"schemaVersion": 1, "playerCount": len(members), "occupiedSeatMask": mask,
	}
	if err := appendEvent(
		ctx, transaction, roomID, &sessionID, &hostProfileID, intPointer(1), "SESSION_CREATED", data, now,
	); err != nil {
		return Room{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Room{}, serviceError("start commit", err)
	}
	return service.Room(ctx, roomID, hostProfileID)
}

type startRoomState struct {
	gameID        string
	revisionID    string
	profileID     string
	profileDigest string
	maxPlayers    int64
}

type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadStartRoom(
	ctx context.Context,
	queryer rowQueryer,
	roomID, hostProfileID string,
	expectedVersion int64,
) (startRoomState, error) {
	var result startRoomState
	var state, host string
	var version int64
	if err := queryer.QueryRowContext(ctx, `
SELECT state,host_profile_id,version,selected_game_id,selected_game_variant_revision_id,
netplay_profile_id,profile_digest,max_players FROM netplay_rooms WHERE id=?
`, roomID).Scan(
		&state, &host, &version, &result.gameID, &result.revisionID,
		&result.profileID, &result.profileDigest, &result.maxPlayers,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return startRoomState{}, ErrRoomNotFound
		}
		return startRoomState{}, serviceError("start room", err)
	}
	if host != hostProfileID {
		return startRoomState{}, ErrForbidden
	}
	if version != expectedVersion {
		return startRoomState{}, ErrPrecondition
	}
	if state != RoomStateWaiting {
		return startRoomState{}, ErrRoomConflict
	}
	return result, nil
}

func (service *Service) lockedStartProfile(
	ctx context.Context, state startRoomState,
) (*eligibleProfile, []byte, string, error) {
	eligible, err := service.eligibleProfiles(ctx, state.gameID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("netplay/start profiles: %w", err)
	}
	var locked *eligibleProfile
	for index := range eligible {
		if eligible[index].Manifest.ID == state.profileID && eligible[index].VariantRevisionID == state.revisionID {
			locked = &eligible[index]
		}
	}
	if locked == nil {
		return nil, nil, "", ErrProfileStale
	}
	canonical, digest, err := service.registry.CanonicalProfile(CanonicalProfileInput{
		ManifestProfile: locked.Manifest, TargetContractSHA256: locked.TargetContractSHA256,
		GameVariantRevisionID: locked.VariantRevisionID, DependencySnapshotJSON: locked.DependencySnapshotJSON,
	})
	if err != nil || digest != state.profileDigest {
		return nil, nil, "", ErrProfileStale
	}
	return locked, canonical, digest, nil
}

type lockedMember struct {
	id      string
	profile string
	player  int
}

func loadLockedMembers(
	ctx context.Context, transaction *sql.Tx, roomID string, maxPlayers int64,
) ([]lockedMember, int, error) {
	memberRows, err := transaction.QueryContext(ctx, `
SELECT id,profile_id,player_no,ready FROM netplay_room_members
WHERE room_id=? AND left_at_ms IS NULL ORDER BY player_no
`, roomID)
	if err != nil {
		return nil, 0, serviceError("start members", err)
	}
	defer func() { cleanup.Error("close", memberRows.Close()) }()
	members := make([]lockedMember, 0, 4)
	mask := 0
	allReady := true
	for memberRows.Next() {
		var member lockedMember
		var ready int
		if err := memberRows.Scan(&member.id, &member.profile, &member.player, &ready); err != nil {
			return nil, 0, serviceError("start member row", err)
		}
		allReady = allReady && ready == 1
		mask |= 1 << (member.player - 1)
		members = append(members, member)
	}
	if err := memberRows.Err(); err != nil {
		return nil, 0, serviceError("start members", err)
	}
	if len(members) < 2 || len(members) > int(maxPlayers) || !allReady {
		return nil, 0, ErrRoomNotReady
	}
	return members, mask, nil
}

func createNetplaySession(
	ctx context.Context,
	transaction *sql.Tx,
	roomID string,
	state startRoomState,
	locked *eligibleProfile,
	canonical []byte,
	digest string,
	members []lockedMember,
	mask int,
	now int64,
) (string, error) {
	var sessionNo int
	if err := transaction.QueryRowContext(ctx, `
SELECT COALESCE(max(session_no),0)+1 FROM netplay_sessions WHERE room_id=?
`, roomID).Scan(&sessionNo); err != nil {
		return "", serviceError("start session number", err)
	}
	sessionID := newV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO netplay_sessions(id,room_id,session_no,state,game_id,game_variant_revision_id,
provider_id,target_id,target_contract_sha256,netplay_compatibility_line,
netplay_profile_id,profile_json,profile_digest,player_count,occupied_seat_mask,
authority_player_no,resync_count,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,'PREPARING',?,?,?,?,?,?,?,?,?,?,?,1,0,1,?,?)
`, sessionID, roomID, sessionNo, state.gameID, state.revisionID,
		locked.Manifest.ProviderID, locked.Manifest.TargetID, locked.TargetContractSHA256,
		locked.Manifest.NetplayCompatibilityLine, state.profileID,
		string(canonical), digest, len(members), mask, now, now); err != nil {
		return "", fmt.Errorf("netplay/create session: %w", err)
	}
	for _, member := range members {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO netplay_session_participants(netplay_session_id,profile_id,room_member_id,player_no,
state,credential_generation,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,'LOCKED',0,1,?,?)
	`, sessionID, member.profile, member.id, member.player, now, now); err != nil {
			return "", serviceError("start participant", err)
		}
	}
	return sessionID, nil
}

func (service *Service) EndRoom(
	ctx context.Context,
	roomID, actorProfileID, reason string,
	expectedVersion *int64,
) error {
	return service.endRoom(ctx, roomID, actorProfileID, reason, expectedVersion, true)
}

func (service *Service) endRoomSystem(ctx context.Context, roomID, reason string) error {
	return service.endRoom(ctx, roomID, "", reason, nil, false)
}

func (service *Service) endRoom(
	ctx context.Context, roomID, actorProfileID, reason string, expectedVersion *int64, authorize bool,
) error {
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("end room transaction", err)
	}
	defer cleanup.Rollback(transaction)
	state, err := service.loadEndRoomState(ctx, transaction, roomID, actorProfileID, authorize)
	if err != nil {
		return err
	}
	if expectedVersion != nil && state.version != *expectedVersion {
		return ErrPrecondition
	}
	if state.terminal {
		return nil
	}
	reason = normalizeEndReason(reason, state.actorIsHost)
	disposition := endDisposition(reason, state.actorIsHost)
	sessionState := endSessionState(reason)
	if state.sessionID.Valid {
		if err := closeNetplaySession(ctx, transaction, state.sessionID.String, sessionState, reason, now); err != nil {
			return err
		}
	}
	if err := service.applyEndDisposition(
		ctx, transaction, roomID, actorProfileID, reason, sessionState, disposition, state.sessionID, now,
	); err != nil {
		return err
	}
	var actor *string
	if authorize {
		actor = &actorProfileID
	}
	if err := appendRoomEndedEvent(
		ctx, transaction, roomID, reason, disposition, state.sessionID, actor, now,
	); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return serviceError("end room commit", err)
	}
	return nil
}

func normalizeEndReason(reason string, actorIsHost bool) string {
	if reason == "PEER_TIMEOUT" && actorIsHost {
		return "HOST_LOST"
	}
	return reason
}

func (service *Service) applyEndDisposition(
	ctx context.Context,
	transaction *sql.Tx,
	roomID, actorProfileID, reason, sessionState, disposition string,
	sessionID sql.NullString,
	now int64,
) error {
	if disposition == RoomDispositionWaiting {
		return service.returnRoomToWaiting(
			ctx, transaction, roomID, actorProfileID, reason, sessionState, sessionID, now,
		)
	}
	return endRoomRecord(ctx, transaction, roomID, reason, now)
}

func appendRoomEndedEvent(
	ctx context.Context,
	transaction *sql.Tx,
	roomID, reason, disposition string,
	sessionID sql.NullString,
	actor *string,
	now int64,
) error {
	if disposition != RoomDispositionEnded {
		return nil
	}
	data := map[string]any{"schemaVersion": 1, "reason": reason}
	return appendEvent(
		ctx, transaction, roomID, stringPointerFromNull(sessionID), actor, nil, "ROOM_ENDED", data, now,
	)
}

type endRoomState struct {
	sessionID   sql.NullString
	version     int64
	actorIsHost bool
	terminal    bool
}

func (service *Service) loadEndRoomState(
	ctx context.Context,
	transaction *sql.Tx,
	roomID, actorProfileID string,
	authorize bool,
) (endRoomState, error) {
	var state, host string
	var result endRoomState
	err := transaction.QueryRowContext(ctx, `
SELECT state,host_profile_id,version,current_session_id FROM netplay_rooms WHERE id=?
`, roomID).Scan(&state, &host, &result.version, &result.sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return endRoomState{}, ErrRoomNotFound
	}
	if err != nil {
		return endRoomState{}, serviceError("end room state", err)
	}
	result.actorIsHost = actorProfileID != "" && actorProfileID == host
	result.terminal = state == "ENDED" || state == "EXPIRED"
	if !authorize || actorProfileID == host {
		return result, nil
	}
	var member int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM netplay_room_members
WHERE room_id=? AND profile_id=? AND left_at_ms IS NULL
`, roomID, actorProfileID).Scan(&member); err != nil {
		return endRoomState{}, serviceError("authorize room end", err)
	}
	if member != 1 {
		return endRoomState{}, ErrForbidden
	}
	return result, nil
}

func endSessionState(reason string) string {
	if reason == "NORMAL" || reason == "USER_EXIT" {
		return "FINISHED"
	}
	return "FAILED"
}

func closeNetplaySession(
	ctx context.Context,
	transaction *sql.Tx,
	sessionID, sessionState, reason string,
	now int64,
) error {
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_sessions SET state=?,finished_at_ms=?,end_reason=?,version=version+1,updated_at_ms=?
WHERE id=? AND state NOT IN ('FINISHED','FAILED')
`, sessionState, now, reason, now, sessionID); err != nil {
		return serviceError("finish session", err)
	}
	playState := "ABANDONED"
	if sessionState == "FINISHED" {
		playState = "FINISHED"
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE play_sessions
SET state=?,ended_at_ms=?,updated_at_ms=?,version=version+1
WHERE launch_session_id IN (
  SELECT id FROM launch_sessions WHERE netplay_session_id=?
)
AND state='ACTIVE'
`, playState, now, now, sessionID); err != nil {
		return serviceError("finish session plays", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions SET state='REVOKED',finished_at_ms=?,updated_at_ms=?,version=version+1
WHERE netplay_session_id=? AND state IN ('CREATED','ACTIVE')
`, now, now, sessionID); err != nil {
		return serviceError("revoke session launches", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_session_participants SET state='LEFT',disconnected_at_ms=NULL,lease_expires_at_ms=NULL,
version=version+1,updated_at_ms=? WHERE netplay_session_id=? AND state!='LEFT'
`, now, sessionID); err != nil {
		return serviceError("close session participants", err)
	}
	return nil
}

func (service *Service) returnRoomToWaiting(
	ctx context.Context,
	transaction *sql.Tx,
	roomID, actorProfileID, reason, sessionState string,
	sessionID sql.NullString,
	now int64,
) error {
	if actorProfileID != "" && guestLeavesAfterSession(reason) {
		leaveReason := guestLeaveReason(reason)
		if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_room_members SET ready=0,left_at_ms=?,leave_reason=?,version=version+1,updated_at_ms=?
WHERE room_id=? AND profile_id=? AND role='GUEST' AND left_at_ms IS NULL
`, now, leaveReason, now, roomID, actorProfileID); err != nil {
			return serviceError("release guest after session", err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_room_members SET ready=0,version=version+1,updated_at_ms=?
WHERE room_id=? AND left_at_ms IS NULL
`, now, roomID); err != nil {
		return serviceError("clear room ready state", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET state='WAITING',current_session_id=NULL,version=version+1,
expires_at_ms=?,updated_at_ms=? WHERE id=?
`, now+service.options.WaitingIdle.Milliseconds(), now, roomID); err != nil {
		return serviceError("return room to waiting", err)
	}
	if !sessionID.Valid {
		return nil
	}
	data := map[string]any{"schemaVersion": 1, "toState": sessionState, "reason": reason}
	return appendEvent(
		ctx, transaction, roomID, stringPointerFromNull(sessionID), nil, nil, "SESSION_STATE_CHANGED", data, now,
	)
}

func guestLeavesAfterSession(reason string) bool {
	return slices.Contains([]string{
		"USER_EXIT", "PEER_TIMEOUT", "AUTH_REVOKED", "PROTOCOL_VIOLATION", "PEER_TOO_SLOW",
	}, reason)
}

func guestLeaveReason(reason string) string {
	switch reason {
	case "USER_EXIT":
		return "USER_LEFT"
	case "AUTH_REVOKED":
		return "AUTH_REVOKED"
	default:
		return "SESSION_ENDED"
	}
}

func endRoomRecord(ctx context.Context, transaction *sql.Tx, roomID, reason string, now int64) error {
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_room_members SET ready=0,left_at_ms=?,leave_reason='ROOM_ENDED',version=version+1,updated_at_ms=?
WHERE room_id=? AND left_at_ms IS NULL
`, now, now, roomID); err != nil {
		return serviceError("end room members", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET state='ENDED',current_session_id=NULL,ended_at_ms=?,end_reason=?,
version=version+1,updated_at_ms=? WHERE id=?
`, now, reason, now, roomID); err != nil {
		return serviceError("end room", err)
	}
	return nil
}

func (service *Service) Leave(ctx context.Context, roomID, profileID string, expectedVersion int64) error {
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("leave room transaction", err)
	}
	defer cleanup.Rollback(transaction)
	var state, role string
	var version int64
	var memberID string
	if err := transaction.QueryRowContext(ctx, `
SELECT room.state,room.version,member.id,member.role
FROM netplay_rooms room JOIN netplay_room_members member ON member.room_id=room.id
WHERE room.id=? AND member.profile_id=? AND member.left_at_ms IS NULL
`, roomID, profileID).Scan(&state, &version, &memberID, &role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrForbidden
		}
		return serviceError("leave room member", err)
	}
	if role == "HOST" {
		return ErrForbidden
	}
	if version != expectedVersion {
		return ErrPrecondition
	}
	if state == RoomStateStarting || state == RoomStateRunning {
		cleanup.Rollback(transaction)
		return service.EndRoom(ctx, roomID, profileID, "USER_EXIT", nil)
	}
	if state != RoomStateWaiting {
		return ErrRoomConflict
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_room_members SET ready=0,left_at_ms=?,leave_reason='USER_LEFT',version=version+1,updated_at_ms=?
WHERE id=?
	`, now, now, memberID); err != nil {
		return serviceError("leave room member update", err)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET version=version+1,expires_at_ms=?,updated_at_ms=? WHERE id=? AND version=?
`, now+service.options.WaitingIdle.Milliseconds(), now, roomID, expectedVersion)
	if err != nil {
		return serviceError("leave room version", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrPrecondition
	}
	if err := appendEvent(ctx, transaction, roomID, nil, &profileID, nil, "MEMBER_LEFT", nil, now); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return serviceError("leave room commit", err)
	}
	return nil
}

func (service *Service) Kick(
	ctx context.Context, roomID, hostProfileID, memberID string, expectedVersion int64,
) error {
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("kick member transaction", err)
	}
	defer cleanup.Rollback(transaction)
	var state, host string
	var version int64
	if err := transaction.QueryRowContext(
		ctx, `SELECT state,host_profile_id,version FROM netplay_rooms WHERE id=?`, roomID,
	).
		Scan(&state, &host, &version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRoomNotFound
		}
		return serviceError("kick member room", err)
	}
	if host != hostProfileID {
		return ErrForbidden
	}
	if version != expectedVersion {
		return ErrPrecondition
	}
	if state != RoomStateWaiting {
		return ErrRoomConflict
	}
	var targetProfile string
	var playerNo int
	if err := transaction.QueryRowContext(ctx, `
SELECT profile_id,player_no FROM netplay_room_members
WHERE id=? AND room_id=? AND role='GUEST' AND left_at_ms IS NULL
`, memberID, roomID).Scan(&targetProfile, &playerNo); err != nil {
		return ErrForbidden
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_room_members SET ready=0,left_at_ms=?,leave_reason='HOST_KICKED',version=version+1,updated_at_ms=?
WHERE id=?
	`, now, now, memberID); err != nil {
		return serviceError("kick member update", err)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET version=version+1,expires_at_ms=?,updated_at_ms=? WHERE id=? AND version=?
`, now+service.options.WaitingIdle.Milliseconds(), now, roomID, expectedVersion)
	if err != nil {
		return serviceError("kick member room version", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrPrecondition
	}
	if err := appendEvent(
		ctx, transaction, roomID, nil, &targetProfile, &playerNo, "MEMBER_KICKED", nil, now,
	); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return serviceError("kick member commit", err)
	}
	return nil
}

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
	defer cleanup.Rollback(transaction)
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
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_sessions SET state=?,version=version+1,updated_at_ms=? WHERE id=?
	`, target, now, sessionID); err != nil {
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
	defer cleanup.Rollback(transaction)
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
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_sessions SET state='RESYNCHRONIZING',resync_count=resync_count+1,
version=version+1,updated_at_ms=? WHERE id=? AND room_id=? AND state=?
`, now, sessionID, roomID, fromState)
	if err != nil {
		return serviceError("prepare resync session", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrRoomConflict
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_session_participants SET state='RUNTIME_READY',disconnected_at_ms=NULL,
lease_expires_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE netplay_session_id=? AND state IN ('CONNECTED','DISCONNECTED')
	`, now, sessionID); err != nil {
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
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_session_participants SET state='DISCONNECTED',disconnected_at_ms=?,lease_expires_at_ms=?,
version=version+1,updated_at_ms=? WHERE netplay_session_id=? AND profile_id=? AND state='CONNECTED'
	`, now, leaseExpires, now, participant.SessionID, participant.ProfileID); err != nil {
		return serviceError("mark disconnected participant", err)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_sessions SET state='PAUSED_RECONNECT',version=version+1,updated_at_ms=?
WHERE id=? AND room_id=? AND state='RUNNING'
`, now, participant.SessionID, participant.RoomID)
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
