package netplay

import (
	"context"
	"fmt"
	"time"
)

type SessionControl struct {
	repository     SessionControlRepository
	reconnectLease time.Duration
	now            func() time.Time
}

func NewSessionControl(
	repository SessionControlRepository,
	reconnectLease time.Duration,
	now func() time.Time,
) *SessionControl {
	return &SessionControl{repository, reconnectLease, now}
}

func (service *SessionControl) mutate(
	ctx context.Context,
	roomID, sessionID string,
	work func(SessionControlScope, SessionControlSnapshot, int64) error,
) error {
	now := service.now().UnixMilli()
	err := service.repository.WithControl(ctx, func(scope SessionControlScope) error {
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
		return ErrRoomConflict
	}
	return service.mutate(
		ctx,
		roomID,
		sessionID,
		func(scope SessionControlScope, before SessionControlSnapshot, now int64) error {
			if before.HostID != actorID {
				return ErrForbidden
			}
			if target == "PAUSED_RECONNECT" && before.State != "RUNNING" ||
				target == "RUNNING" && before.State != "PAUSED_RECONNECT" {
				return ErrRoomConflict
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
				SessionTransitionPlan{Before: before, Target: target, Events: []SessionEvent{event}, Now: now},
			)
		},
	)
}

func (service *SessionControl) Resync(ctx context.Context, roomID, sessionID string, cause ResyncCause) error {
	return service.mutate(
		ctx,
		roomID,
		sessionID,
		func(scope SessionControlScope, before SessionControlSnapshot, now int64) error {
			if !ValidResyncSource(cause, before.State) {
				return ErrRoomConflict
			}
			kind := "RESUMED"
			if cause == ResyncHash {
				kind = "PAUSED"
			}
			event := stateEvent(kind, before.State, "RESYNCHRONIZING", string(cause))
			return writeSessionControl(
				ctx,
				scope.Write,
				SessionTransitionPlan{
					Before:          before,
					Target:          "RESYNCHRONIZING",
					IncrementResync: true,
					PeerMode:        PeersPrepareResync,
					Events:          []SessionEvent{event},
					Now:             now,
				},
			)
		},
	)
}

func ValidResyncSource(cause ResyncCause, state string) bool {
	switch cause {
	case ResyncReconnect:
		return state == "PAUSED_RECONNECT" || state == "RUNNING"
	case ResyncHash:
		return state == "RUNNING"
	case ResyncHost:
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
		func(scope SessionControlScope, before SessionControlSnapshot, now int64) error {
			if before.State != "SYNCHRONIZING" && before.State != "RESYNCHRONIZING" {
				return ErrRoomConflict
			}
			events := []SessionEvent{stateEvent("SESSION_STATE_CHANGED", before.State, "RUNNING", "")}
			if before.State == "RESYNCHRONIZING" {
				events = append(
					events,
					SessionEvent{Type: "RESYNCED", Data: SessionEventData{SchemaVersion: 1, ResyncCount: &before.ResyncCount}},
				)
			}
			return writeSessionControl(
				ctx,
				scope.Write,
				SessionTransitionPlan{
					Before:   before,
					Target:   "RUNNING",
					Started:  true,
					PeerMode: PeersConnect,
					Events:   events,
					Now:      now,
				},
			)
		},
	)
}

func stateEvent(kind, from, to, reason string) SessionEvent {
	return SessionEvent{Type: kind, Data: SessionEventData{SchemaVersion: 1, FromState: from, ToState: to, Reason: reason}}
}

func writeSessionControl(ctx context.Context, writer SessionControlWriter, plan SessionTransitionPlan) error {
	if err := writer.Session(ctx, plan); err != nil {
		return fmt.Errorf("netplay/write session transition: %w", err)
	}
	return nil
}

func writePeerControl(ctx context.Context, writer SessionControlWriter, plan PeerTransitionPlan) error {
	if err := writer.Peer(ctx, plan); err != nil {
		return fmt.Errorf("netplay/write peer transition: %w", err)
	}
	return nil
}
