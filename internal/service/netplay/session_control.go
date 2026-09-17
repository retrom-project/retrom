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

func (service *SessionControl) mutate(
	ctx context.Context,
	roomID, sessionID string,
	work func(model.SessionControlScope, model.SessionControlSnapshot, int64) error,
) error {
	now := service.now().UnixMilli()
	err := service.repository.WithControl(ctx, func(scope model.SessionControlScope) error {
		before, err := scope.Read.Current(ctx, roomID, sessionID)
		if err != nil {
			return fmt.Errorf("netplay/read session control: %w", err)
		}
		return work(scope, before, now)
	})
	if err != nil {
		return fmt.Errorf("netplay/session control: %w", err)
	}
	return nil
}

func (service *SessionControl) SetState(ctx context.Context, roomID, sessionID, actorID, target string) error {
	if target != "PAUSED_RECONNECT" && target != "RUNNING" {
		return model.ErrRoomConflict
	}
	return service.mutate(
		ctx,
		roomID,
		sessionID,
		func(scope model.SessionControlScope, before model.SessionControlSnapshot, now int64) error {
			if before.HostID != actorID {
				return model.ErrForbidden
			}
			if target == "PAUSED_RECONNECT" && before.State != "RUNNING" ||
				target == "RUNNING" && before.State != "PAUSED_RECONNECT" {
				return model.ErrRoomConflict
			}
			kind := "PAUSED"
			if target == "RUNNING" {
				kind = "RESUMED"
			}
			event := stateEvent(kind, before.State, target, "")
			event.ActorID = &actorID
			player := 1
			event.PlayerNo = &player
			return writeSessionControl(
				ctx,
				scope.Write,
				model.SessionTransitionPlan{
					Before: before,
					Target: target,
					Events: []model.SessionEvent{event},
					Now:    now,
				},
			)
		},
	)
}

func (service *SessionControl) Resync(ctx context.Context, roomID, sessionID string, cause model.ResyncCause) error {
	return service.mutate(
		ctx,
		roomID,
		sessionID,
		func(scope model.SessionControlScope, before model.SessionControlSnapshot, now int64) error {
			if !ValidResyncSource(cause, before.State) {
				return model.ErrRoomConflict
			}
			kind := "RESUMED"
			if cause == model.ResyncHash {
				kind = "PAUSED"
			}
			event := stateEvent(kind, before.State, "RESYNCHRONIZING", string(cause))
			return writeSessionControl(
				ctx,
				scope.Write,
				model.SessionTransitionPlan{
					Before:          before,
					Target:          "RESYNCHRONIZING",
					IncrementResync: true,
					PeerMode:        model.PeersPrepareResync,
					Events:          []model.SessionEvent{event},
					Now:             now,
				},
			)
		},
	)
}

func ValidResyncSource(cause model.ResyncCause, state string) bool {
	switch cause {
	case model.ResyncReconnect:
		return state == "PAUSED_RECONNECT" || state == "RUNNING"
	case model.ResyncHash:
		return state == "RUNNING"
	case model.ResyncHost:
		return state == "PAUSED_RECONNECT"
	default:
		return false
	}
}

func (service *SessionControl) Running(ctx context.Context, roomID, sessionID string) error {
	return service.mutate(
		ctx,
		roomID,
		sessionID,
		func(scope model.SessionControlScope, before model.SessionControlSnapshot, now int64) error {
			if before.State != "SYNCHRONIZING" && before.State != "RESYNCHRONIZING" {
				return model.ErrRoomConflict
			}
			events := []model.SessionEvent{stateEvent("SESSION_STATE_CHANGED", before.State, "RUNNING", "")}
			if before.State == "RESYNCHRONIZING" {
				events = append(
					events,
					model.SessionEvent{
						Type: "RESYNCED",
						Data: model.SessionEventData{SchemaVersion: 1, ResyncCount: &before.ResyncCount},
					},
				)
			}
			return writeSessionControl(
				ctx,
				scope.Write,
				model.SessionTransitionPlan{
					Before:   before,
					Target:   "RUNNING",
					Started:  true,
					PeerMode: model.PeersConnect,
					Events:   events,
					Now:      now,
				},
			)
		},
	)
}

func stateEvent(kind, from, to, reason string) model.SessionEvent {
	return model.SessionEvent{
		Type: kind,
		Data: model.SessionEventData{SchemaVersion: 1, FromState: from, ToState: to, Reason: reason},
	}
}

func writeSessionControl(
	ctx context.Context,
	writer model.SessionControlWriter,
	plan model.SessionTransitionPlan,
) error {
	if err := writer.Session(ctx, plan); err != nil {
		return fmt.Errorf("netplay/write session transition: %w", err)
	}
	return nil
}

func writePeerControl(ctx context.Context, writer model.SessionControlWriter, plan model.PeerTransitionPlan) error {
	if err := writer.Peer(ctx, plan); err != nil {
		return fmt.Errorf("netplay/write peer transition: %w", err)
	}
	return nil
}
