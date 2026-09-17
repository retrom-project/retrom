package netplay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	model "retrom/internal/model/netplay"
)

type RoomExit struct {
	repository  model.RoomExitRepository
	waitingIdle time.Duration
	now         func() time.Time
}

func NewRoomExit(repository model.RoomExitRepository, waitingIdle time.Duration, now func() time.Time) *RoomExit {
	return &RoomExit{repository, waitingIdle, now}
}

func (service *RoomExit) End(ctx context.Context, roomID, actorID, reason string, version *int64) error {
	return service.end(ctx, roomID, &actorID, reason, version)
}

func (service *RoomExit) EndSystem(ctx context.Context, roomID, reason string) error {
	return service.end(ctx, roomID, nil, reason, nil)
}

func (service *RoomExit) end(ctx context.Context, roomID string, actor *string, reason string, version *int64) error {
	actorID := ""
	if actor != nil {
		actorID = *actor
	}
	now := service.now().UnixMilli()
	err := service.repository.WithExit(ctx, func(scope model.RoomExitScope) error {
		before, err := scope.Read.Current(ctx, roomID, actorID)
		if err != nil {
			return fmt.Errorf("netplay/read exit state: %w", err)
		}
		if actor != nil && actorID != before.Room.HostID {
			if reason == "HOST_CLOSED" || !activeExitMember(before.Room.Member) {
				return model.ErrForbidden
			}
		}
		if version != nil && before.Room.Version != *version {
			return model.ErrPrecondition
		}
		return service.finish(ctx, scope.Write, before, actor, reason, now)
	})
	if err != nil {
		return fmt.Errorf("netplay/end room: %w", err)
	}
	return nil
}

func activeExitMember(member *model.SeatMember) bool { return member != nil && member.LeftAtMS == nil }

func (service *RoomExit) finish(
	ctx context.Context,
	writer model.RoomExitWriter,
	before model.RoomExitSnapshot,
	actor *string,
	reason string,
	now int64,
) error {
	if before.Room.State == "ENDED" || before.Room.State == "EXPIRED" {
		return nil
	}
	host := actor != nil && *actor == before.Room.HostID
	if reason == "PEER_TIMEOUT" && host {
		reason = "HOST_LOST"
	}
	disposition := EndDisposition(reason, host)
	if disposition == "WAITING" && before.Room.Selection == nil {
		return model.ErrRoomConflict
	}
	plan := model.RoomEndPlan{
		Before:       before,
		ActorID:      actor,
		Reason:       reason,
		Disposition:  disposition,
		SessionState: "FAILED",
		PlayState:    "ABANDONED",
		Now:          now,
		ExpiresAtMS:  now + service.waitingIdle.Milliseconds(),
	}
	if reason == "NORMAL" || reason == "USER_EXIT" {
		plan.SessionState = "FINISHED"
		plan.PlayState = "FINISHED"
	}
	plan.LeaveProfileID, plan.LeaveReason = departingGuest(actor, host, disposition, reason)
	event := struct {
		SchemaVersion int    `json:"schemaVersion"`
		ToState       string `json:"toState,omitempty"`
		Reason        string `json:"reason"`
	}{SchemaVersion: 1, Reason: reason}
	if disposition == "WAITING" {
		event.ToState = plan.SessionState
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("netplay/encode end event: %w", err)
	}
	plan.Event = data
	if err := writer.End(ctx, plan); err != nil {
		return fmt.Errorf("netplay/write end: %w", err)
	}
	return nil
}

func EndDisposition(reason string, actorIsHost bool) string {
	switch reason {
	case "HOST_CLOSED", "HOST_LOST", "PROFILE_REVOKED", "SERVER_RESTARTED", "RESTORE", "HARD_EXPIRED":
		return "ENDED"
	case "AUTH_REVOKED", "PEER_TIMEOUT", "PROTOCOL_VIOLATION":
		if actorIsHost {
			return "ENDED"
		}
	}
	return "WAITING"
}

func (service *RoomExit) Leave(ctx context.Context, roomID, actorID string, version int64) error {
	now := service.now().UnixMilli()
	err := service.repository.WithExit(ctx, func(scope model.RoomExitScope) error {
		before, err := scope.Read.Current(ctx, roomID, actorID)
		if errors.Is(err, model.ErrRoomNotFound) {
			return model.ErrForbidden
		}
		if err != nil {
			return fmt.Errorf("netplay/read exit state: %w", err)
		}
		member := before.Room.Member
		if !activeExitMember(member) || member.Role == "HOST" {
			return model.ErrForbidden
		}
		if before.Room.Version != version {
			return model.ErrPrecondition
		}
		if before.Room.State == model.RoomStateStarting || before.Room.State == model.RoomStateRunning {
			return service.finish(ctx, scope.Write, before, &actorID, "USER_EXIT", now)
		}
		if before.Room.State != model.RoomStateWaiting {
			return model.ErrRoomConflict
		}
		return scope.Write.Remove(
			ctx,
			model.RoomRemovalPlan{
				Before: before.Room,
				Member: *member,
				Reason: "USER_LEFT",
				Evidence: model.RoomControlEvidence{
					ActorID:     actorID,
					Type:        "MEMBER_LEFT",
					Data:        []byte(`{"schemaVersion":1}`),
					Now:         now,
					ExpiresAtMS: now + service.waitingIdle.Milliseconds(),
				},
			},
		)
	})
	if err != nil {
		return fmt.Errorf("netplay/leave room: %w", err)
	}
	return nil
}

func (service *RoomExit) Kick(ctx context.Context, roomID, actorID, memberID string, version int64) error {
	now := service.now().UnixMilli()
	err := service.repository.WithExit(ctx, func(scope model.RoomExitScope) error {
		before, err := scope.Read.Current(ctx, roomID, actorID)
		if err != nil {
			return fmt.Errorf("netplay/read exit state: %w", err)
		}
		if before.Room.HostID != actorID {
			return model.ErrForbidden
		}
		if before.Room.Version != version {
			return model.ErrPrecondition
		}
		if before.Room.State != model.RoomStateWaiting {
			return model.ErrRoomConflict
		}
		for _, member := range before.Room.Occupants {
			if member.ID != memberID || member.Role != "GUEST" || member.LeftAtMS != nil {
				continue
			}
			return scope.Write.Remove(
				ctx,
				model.RoomRemovalPlan{
					Before: before.Room,
					Member: member,
					Reason: "HOST_KICKED",
					Evidence: model.RoomControlEvidence{
						ActorID:     member.ProfileID,
						PlayerNo:    &member.PlayerNo,
						Type:        "MEMBER_KICKED",
						Data:        []byte(`{"schemaVersion":1}`),
						Now:         now,
						ExpiresAtMS: now + service.waitingIdle.Milliseconds(),
					},
				},
			)
		}
		return model.ErrForbidden
	})
	if err != nil {
		return fmt.Errorf("netplay/kick member: %w", err)
	}
	return nil
}

func departingGuest(actor *string, host bool, disposition, reason string) (string, string) {
	if disposition != "WAITING" || actor == nil || host {
		return "", ""
	}
	if !slices.Contains(
		[]string{"USER_EXIT", "PEER_TIMEOUT", "AUTH_REVOKED", "PROTOCOL_VIOLATION", "PEER_TOO_SLOW"},
		reason,
	) {
		return "", ""
	}
	switch reason {
	case "USER_EXIT":
		return *actor, "USER_LEFT"
	case "AUTH_REVOKED":
		return *actor, "AUTH_REVOKED"
	default:
		return *actor, "SESSION_ENDED"
	}
}
