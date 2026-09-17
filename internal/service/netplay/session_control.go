package netplay

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/netplay"
)

type SessionControl struct {
	repository     model.SessionControlRepository
	reconnectLease time.Duration
	now            func() time.Time
}

func NewSessionControl(
	repository model.SessionControlRepository,
	reconnectLease time.Duration,
	now func() time.Time,
) *SessionControl {
	return &SessionControl{repository, reconnectLease, now}
}

func (service *SessionControl) SetState(
	ctx context.Context,
	roomID, sessionID, actorID, target string,
) error {
	if target != "PAUSED_RECONNECT" && target != "RUNNING" {
		return model.ErrRoomConflict
	}
	err := service.repository.CommitSetSessionState(ctx, model.SetSessionStateCommand{
		RoomID:    roomID,
		SessionID: sessionID,
		ActorID:   actorID,
		Target:    target,
		NowMS:     service.now().UnixMilli(),
	})
	if err != nil {
		return fmt.Errorf("netplay/session control: %w", err)
	}
	return nil
}

func (service *SessionControl) Resync(
	ctx context.Context,
	roomID, sessionID string,
	cause model.ResyncCause,
) error {
	err := service.repository.CommitResync(ctx, model.ResyncCommand{
		RoomID:    roomID,
		SessionID: sessionID,
		Cause:     cause,
		NowMS:     service.now().UnixMilli(),
	})
	if err != nil {
		return fmt.Errorf("netplay/session control: %w", err)
	}
	return nil
}

func (service *SessionControl) Running(
	ctx context.Context,
	roomID, sessionID string,
) error {
	err := service.repository.CommitRunning(ctx, model.RunningCommand{
		RoomID:    roomID,
		SessionID: sessionID,
		NowMS:     service.now().UnixMilli(),
	})
	if err != nil {
		return fmt.Errorf("netplay/session control: %w", err)
	}
	return nil
}
