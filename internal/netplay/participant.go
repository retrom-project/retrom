package netplay

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	application "retrom/internal/service/netplay"

	"retrom/internal/dbexec"

	"retrom/internal/persistence/recordstore"

	"retrom/internal/launch"
)

type Event = application.Event

type ParticipantLaunch struct {
	Launch           launch.Created
	RoomCapability   string
	CredentialExpiry int64
}

type SocketParticipant = application.SocketParticipant

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
		GameID: spec.gameID, GameVariantID: spec.variantID,
		ProviderID: spec.providerID, TargetID: spec.targetID,
		BundleSHA256: spec.bundleSHA256,
		ReturnTo:     "/netplay/rooms/" + roomID, ClientCapabilities: capabilities,
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
	gameID       string
	variantID    string
	providerID   string
	targetID     string
	bundleSHA256 string
	playerNo     int
	generation   int64
}

func (service *Service) participantLaunchSpec(
	ctx context.Context, roomID, sessionID, profileID string,
) (participantLaunchSpec, error) {
	var spec participantLaunchSpec
	var sessionState, participantState string
	if err := service.database.QueryRowContext(ctx, `
SELECT session.game_id,session.game_variant_id,session.provider_id,session.target_id,
 session.bundle_sha256,session.state,
  participant.player_no,participant.state,participant.credential_generation
FROM netplay_sessions session
JOIN netplay_session_participants participant ON participant.netplay_session_id=session.id
WHERE session.id=? AND session.room_id=? AND participant.profile_id=?
`, sessionID, roomID, profileID).Scan(
		&spec.gameID, &spec.variantID, &spec.providerID, &spec.targetID,
		&spec.bundleSHA256, &sessionState,
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

func (service *Service) recordParticipantLaunch(
	ctx context.Context, roomID, sessionID, profileID string, playerNo int, now int64,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("record participant launch transaction", err)
	}
	defer dbexec.Rollback(transaction)
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
	result, err := recordstore.UpdateNetplaySessions(ctx, transaction, recordstore.Update{
		Set: `state='LOADING',version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND state='PREPARING'`,
			Args:  []any{sessionID},
		},
		Values: []any{now},
	})
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
