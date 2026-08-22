package netplay

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/launch"
)

type Event struct {
	ID        int64          `json:"id"`
	EventType string         `json:"eventType"`
	Data      map[string]any `json:"data"`
	CreatedAt int64          `json:"createdAtMs"`
}

type ParticipantLaunch struct {
	Launch           launch.Created
	RoomCapability   string
	CredentialExpiry int64
}

type SocketParticipant struct {
	RoomID               string
	SessionID            string
	ProfileID            string
	PlayerNo             int
	CredentialGeneration int64
	ProfileDigest        string
	CoreArtifactID       string
	RoomVersion          int64
	SessionVersion       int64
	SessionState         string
	OccupiedSeatMask     int
	PlayerCount          int
}

func (service *Service) AuthenticateSocket(
	ctx context.Context, roomID, profileID, encodedCredential string,
) (SocketParticipant, error) {
	var participant SocketParticipant
	var credentialHash []byte
	var launchState string
	err := service.database.QueryRowContext(ctx, `
SELECT room.id,session.id,participant.profile_id,participant.player_no,
  participant.credential_generation,session.profile_digest,session.core_artifact_id,
  room.version,session.version,session.state,session.occupied_seat_mask,session.player_count,
  participant.credential_sha256,launch.state
FROM netplay_rooms room
JOIN netplay_sessions session ON session.id=room.current_session_id AND session.room_id=room.id
JOIN netplay_session_participants participant ON participant.netplay_session_id=session.id
JOIN launch_sessions launch ON launch.id=participant.launch_session_id
WHERE room.id=? AND participant.profile_id=? AND room.state IN ('STARTING','RUNNING')
  AND session.state NOT IN ('FINISHED','FAILED')
`, roomID, profileID).Scan(
		&participant.RoomID, &participant.SessionID, &participant.ProfileID, &participant.PlayerNo,
		&participant.CredentialGeneration, &participant.ProfileDigest, &participant.CoreArtifactID,
		&participant.RoomVersion, &participant.SessionVersion, &participant.SessionState,
		&participant.OccupiedSeatMask, &participant.PlayerCount,
		&credentialHash, &launchState,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SocketParticipant{}, ErrForbidden
	}
	if err != nil {
		return SocketParticipant{}, serviceError("authenticate socket", err)
	}
	if launchState != "ACTIVE" || !MatchesCapability(encodedCredential, credentialHash) {
		return SocketParticipant{}, ErrForbidden
	}
	return participant, nil
}

func (service *Service) MarkRuntimeReady(ctx context.Context, participant SocketParticipant) (bool, error) {
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return false, serviceError("mark runtime ready transaction", err)
	}
	defer cleanup.Rollback(transaction)
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_session_participants SET state='RUNTIME_READY',version=version+1,updated_at_ms=?
WHERE netplay_session_id=? AND profile_id=? AND state='LAUNCH_READY'
`, now, participant.SessionID, participant.ProfileID)
	if err != nil {
		return false, serviceError("mark runtime ready participant", err)
	}
	changed, _ := result.RowsAffected()
	if changed == 1 {
		if err := appendEvent(ctx, transaction, participant.RoomID, &participant.SessionID,
			&participant.ProfileID, &participant.PlayerNo, "PARTICIPANT_STATE_CHANGED", map[string]any{
				"schemaVersion": 1, "fromState": "LAUNCH_READY", "toState": "RUNTIME_READY",
			}, now); err != nil {
			return false, err
		}
	}
	var remaining int
	var sessionState string
	if err := transaction.QueryRowContext(ctx, `
SELECT
  (SELECT count(*) FROM netplay_session_participants
   WHERE netplay_session_id=? AND state!='RUNTIME_READY'),
  (SELECT state FROM netplay_sessions WHERE id=? AND room_id=? )
	`, participant.SessionID, participant.SessionID, participant.RoomID).Scan(&remaining, &sessionState); err != nil {
		return false, serviceError("mark runtime ready remaining", err)
	}
	allReady := remaining == 0 && sessionState == "LOADING"
	if allReady {
		if err := advanceSessionToSynchronizing(ctx, transaction, participant, now); err != nil {
			return false, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return false, serviceError("mark runtime ready commit", err)
	}
	return allReady, nil
}

func advanceSessionToSynchronizing(
	ctx context.Context, transaction *sql.Tx, participant SocketParticipant, now int64,
) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_sessions SET state='SYNCHRONIZING',version=version+1,updated_at_ms=?
WHERE id=? AND state='LOADING'
`, now, participant.SessionID)
	if err != nil {
		return serviceError("mark runtime ready session", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return nil
	}
	data := map[string]any{
		"schemaVersion": 1, "fromState": "LOADING", "toState": "SYNCHRONIZING",
	}
	return appendEvent(
		ctx, transaction, participant.RoomID, &participant.SessionID, nil, nil,
		"SESSION_STATE_CHANGED", data, now,
	)
}

func (service *Service) MarkSessionRunning(ctx context.Context, roomID, sessionID string) error {
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("mark session running transaction", err)
	}
	defer cleanup.Rollback(transaction)
	var fromState string
	var resyncCount int
	if err := transaction.QueryRowContext(
		ctx, `SELECT state,resync_count FROM netplay_sessions WHERE id=? AND room_id=?`, sessionID, roomID,
	).
		Scan(&fromState, &resyncCount); err != nil {
		return serviceError("mark session running state", err)
	}
	if fromState != "SYNCHRONIZING" && fromState != "RESYNCHRONIZING" {
		return ErrRoomConflict
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_session_participants SET state='CONNECTED',version=version+1,updated_at_ms=?
WHERE netplay_session_id=? AND state IN ('RUNTIME_READY','SYNCHRONIZED')
	`, now, sessionID); err != nil {
		return serviceError("mark session participants connected", err)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_sessions SET state='RUNNING',started_at_ms=COALESCE(started_at_ms,?),version=version+1,updated_at_ms=?
WHERE id=? AND room_id=? AND state IN ('SYNCHRONIZING','RESYNCHRONIZING')
`, now, now, sessionID, roomID)
	if err != nil {
		return serviceError("mark session running", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrRoomConflict
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET state='RUNNING',version=version+1,updated_at_ms=?
WHERE id=? AND current_session_id=? AND state='STARTING'
	`, now, roomID, sessionID); err != nil {
		return serviceError("mark room running", err)
	}
	if err := appendEvent(ctx, transaction, roomID, &sessionID, nil, nil, "SESSION_STATE_CHANGED",
		map[string]any{"schemaVersion": 1, "fromState": fromState, "toState": "RUNNING"}, now); err != nil {
		return err
	}
	if fromState == "RESYNCHRONIZING" {
		if err := appendEvent(ctx, transaction, roomID, &sessionID, nil, nil, "RESYNCED",
			map[string]any{"schemaVersion": 1, "resyncCount": resyncCount}, now); err != nil {
			return err
		}
	}
	if err := transaction.Commit(); err != nil {
		return serviceError("mark session running commit", err)
	}
	return nil
}

func (service *Service) ParticipantCapability(
	ctx context.Context, sessionID, profileID string,
) (string, error) {
	var generation int64
	var credentialHash []byte
	var launchID string
	if err := service.database.QueryRowContext(ctx, `
SELECT credential_generation,credential_sha256,launch_session_id
FROM netplay_session_participants
WHERE netplay_session_id=? AND profile_id=? AND launch_session_id IS NOT NULL
`, sessionID, profileID).Scan(&generation, &credentialHash, &launchID); err != nil {
		return "", ErrForbidden
	}
	sessionUUID, sessionErr := uuid.Parse(sessionID)
	profileUUID, profileErr := uuid.Parse(profileID)
	if sessionErr != nil || profileErr != nil || generation < 1 {
		return "", ErrForbidden
	}
	credential := service.credentials.Capability(sessionUUID, profileUUID, uint32(generation))
	if !MatchesCapability(EncodeCapability(credential), credentialHash) {
		return "", ErrForbidden
	}
	return EncodeCapability(credential), nil
}

func (service *Service) failPreparation(ctx context.Context, roomID string, cause error) error {
	rollbackContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := service.endRoomSystem(rollbackContext, roomID, "PREPARE_FAILED"); err != nil {
		return errors.Join(cause, fmt.Errorf("netplay/abort preparation: %w", err))
	}
	return cause
}

func (service *Service) CreateParticipantLaunch(
	ctx context.Context,
	launcher *launch.Service,
	roomID, sessionID, profileID string,
	capabilities launch.Capabilities,
) (ParticipantLaunch, error) {
	service.launchMu.Lock()
	defer service.launchMu.Unlock()
	spec, err := service.participantLaunchSpec(ctx, roomID, sessionID, profileID)
	if err != nil {
		return ParticipantLaunch{}, err
	}
	credential, err := service.participantCredential(sessionID, profileID, spec.generation)
	if err != nil {
		return ParticipantLaunch{}, err
	}
	credentialHash := HashCapability(credential)
	created, err := launcher.CreateNetplay(ctx, launch.NetplayCreateRequest{
		RoomID: roomID, SessionID: sessionID, ProfileID: profileID, PlayerNo: spec.playerNo,
		GameID: spec.gameID, GameVariantRevisionID: spec.revisionID, CoreArtifactID: spec.artifactID,
		ReturnTo: "/netplay/rooms/" + roomID, ClientCapabilities: capabilities,
		CredentialGeneration: spec.generation, NetplayCredentialSHA256: credentialHash[:],
	})
	if err != nil {
		cause := fmt.Errorf("netplay/create participant launch: %w", err)
		return ParticipantLaunch{}, service.failPreparation(ctx, roomID, cause)
	}
	if err := service.recordParticipantLaunch(
		ctx, roomID, sessionID, profileID, spec.playerNo, service.clock.Now().UnixMilli(),
	); err != nil {
		cause := fmt.Errorf("netplay/record participant launch: %w", err)
		return ParticipantLaunch{}, service.failPreparation(ctx, roomID, cause)
	}
	return ParticipantLaunch{
		Launch: created, RoomCapability: EncodeCapability(credential),
		CredentialExpiry: service.clock.Now().Add(8 * time.Hour).UnixMilli(),
	}, nil
}

type participantLaunchSpec struct {
	gameID     string
	revisionID string
	artifactID string
	playerNo   int
	generation int64
}

func (service *Service) participantLaunchSpec(
	ctx context.Context, roomID, sessionID, profileID string,
) (participantLaunchSpec, error) {
	var spec participantLaunchSpec
	var sessionState, participantState string
	if err := service.database.QueryRowContext(ctx, `
SELECT session.game_id,session.game_variant_revision_id,session.core_artifact_id,session.state,
  participant.player_no,participant.state,participant.credential_generation
FROM netplay_sessions session
JOIN netplay_session_participants participant ON participant.netplay_session_id=session.id
WHERE session.id=? AND session.room_id=? AND participant.profile_id=?
`, sessionID, roomID, profileID).Scan(
		&spec.gameID, &spec.revisionID, &spec.artifactID, &sessionState,
		&spec.playerNo, &participantState, &spec.generation,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return participantLaunchSpec{}, ErrSessionNotFound
		}
		return participantLaunchSpec{}, serviceError("load participant launch", err)
	}
	if sessionState == "FINISHED" || sessionState == "FAILED" ||
		(participantState != "LOCKED" && participantState != "LAUNCH_READY" && participantState != "RUNTIME_READY") {
		return participantLaunchSpec{}, ErrRoomConflict
	}
	if spec.generation == 0 {
		spec.generation = 1
	}
	return spec, nil
}

func (service *Service) participantCredential(
	sessionID, profileID string, generation int64,
) ([32]byte, error) {
	const maxCredentialGeneration = int64(1<<32 - 1)
	if generation < 1 || generation > maxCredentialGeneration {
		return [32]byte{}, ErrForbidden
	}
	sessionUUID, err := uuid.Parse(sessionID)
	if err != nil {
		return [32]byte{}, ErrSessionNotFound
	}
	profileUUID, err := uuid.Parse(profileID)
	if err != nil {
		return [32]byte{}, ErrForbidden
	}
	return service.credentials.Capability(sessionUUID, profileUUID, uint32(generation)), nil
}

func (service *Service) recordParticipantLaunch(
	ctx context.Context, roomID, sessionID, profileID string, playerNo int, now int64,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("record participant launch transaction", err)
	}
	defer cleanup.Rollback(transaction)
	var transitionRecorded int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM netplay_events
WHERE room_id=? AND netplay_session_id=? AND profile_id=? AND event_type='PARTICIPANT_STATE_CHANGED'
`, roomID, sessionID, profileID).Scan(&transitionRecorded); err != nil {
		return serviceError("check participant launch event", err)
	}
	if transitionRecorded == 0 {
		data := map[string]any{
			"schemaVersion": 1, "fromState": "LOCKED", "toState": "LAUNCH_READY",
		}
		if err := appendEvent(
			ctx, transaction, roomID, &sessionID, &profileID, &playerNo,
			"PARTICIPANT_STATE_CHANGED", data, now,
		); err != nil {
			return err
		}
	}
	if err := service.advanceSessionToLoading(ctx, transaction, roomID, sessionID, now); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return serviceError("record participant launch commit", err)
	}
	return nil
}

func (service *Service) advanceSessionToLoading(
	ctx context.Context, transaction *sql.Tx, roomID, sessionID string, now int64,
) error {
	var remaining int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM netplay_session_participants
WHERE netplay_session_id=? AND state='LOCKED'
`, sessionID).Scan(&remaining); err != nil {
		return serviceError("count locked participants", err)
	}
	if remaining != 0 {
		return nil
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_sessions SET state='LOADING',version=version+1,updated_at_ms=? WHERE id=? AND state='PREPARING'
`, now, sessionID)
	if err != nil {
		return serviceError("advance session to loading", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return nil
	}
	data := map[string]any{
		"schemaVersion": 1, "fromState": "PREPARING", "toState": "LOADING",
	}
	return appendEvent(
		ctx, transaction, roomID, &sessionID, nil, nil, "SESSION_STATE_CHANGED", data, now,
	)
}
