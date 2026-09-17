package netplay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	model "retrom/internal/model/netplay"
)

func (service *RoomControl) SetSeat(
	ctx context.Context,
	roomID, actorID string,
	playerNo int,
	version int64,
) (model.Room, error) {
	if playerNo < 2 || playerNo > 4 {
		return model.Room{}, model.ErrInvalidSeat
	}
	request := roomMutation{roomID: roomID, actorID: actorID, version: version, states: []string{model.RoomStateWaiting}}
	return service.mutate(ctx, request, func(scope model.RoomControlScope, before model.RoomControlSnapshot, now int64) error {
		if err := validateSeat(before, playerNo, actorID); err != nil {
			return err
		}
		plan, err := service.seatPlan(before, actorID, playerNo, now)
		if err != nil {
			return err
		}
		if err := scope.Write.Seat(ctx, plan); err != nil {
			return fmt.Errorf("netplay/set seat: %w", err)
		}
		return nil
	})
}

func validateSeat(before model.RoomControlSnapshot, playerNo int, actorID string) error {
	if before.Selection == nil {
		return model.ErrProfileStale
	}
	if playerNo > before.Selection.MaxPlayers {
		return model.ErrInvalidSeat
	}
	if before.Member != nil {
		if before.Member.Ready {
			return model.ErrRoomConflict
		}
		if before.Member.Role == "HOST" {
			return model.ErrForbidden
		}
	}
	for _, member := range before.Occupants {
		if member.PlayerNo == playerNo && member.ProfileID != actorID {
			return model.ErrSeatTaken
		}
	}
	return nil
}

func (service *RoomControl) seatPlan(
	before model.RoomControlSnapshot,
	actorID string,
	playerNo int,
	now int64,
) (model.RoomSeatPlan, error) {
	memberID, eventType := "", "SEAT_CHANGED"
	var fromPlayer *int
	if before.Member != nil {
		memberID = before.Member.ID
		player := before.Member.PlayerNo
		fromPlayer = &player
	} else {
		eventType = "MEMBER_JOINED"
		var err error
		memberID, err = service.newID()
		if err != nil {
			return model.RoomSeatPlan{}, fmt.Errorf("netplay/new member identity: %w", err)
		}
	}
	data, err := json.Marshal(struct {
		SchemaVersion int  `json:"schemaVersion"`
		ToPlayerNo    int  `json:"toPlayerNo"`
		FromPlayerNo  *int `json:"fromPlayerNo,omitempty"`
	}{1, playerNo, fromPlayer})
	if err != nil {
		return model.RoomSeatPlan{}, fmt.Errorf("netplay/seat event: %w", err)
	}
	return model.RoomSeatPlan{
		Before:   before,
		MemberID: memberID,
		PlayerNo: playerNo,
		Evidence: model.RoomControlEvidence{
			ActorID:     actorID,
			Type:        eventType,
			PlayerNo:    &playerNo,
			Data:        data,
			Now:         now,
			ExpiresAtMS: now + service.waitingIdle.Milliseconds(),
		},
	}, nil
}

func (service *RoomControl) SetReady(
	ctx context.Context,
	roomID, actorID string,
	ready bool,
	version int64,
) (model.Room, error) {
	request := roomMutation{roomID: roomID, actorID: actorID, version: version, states: []string{model.RoomStateWaiting}}
	return service.mutate(ctx, request, func(scope model.RoomControlScope, before model.RoomControlSnapshot, now int64) error {
		if before.Member == nil || before.Member.LeftAtMS != nil {
			return model.ErrForbidden
		}
		if ready {
			if err := service.requireCurrentSelection(ctx, scope, before.Selection); err != nil {
				return err
			}
		}
		data, err := json.Marshal(struct {
			SchemaVersion int  `json:"schemaVersion"`
			Ready         bool `json:"ready"`
		}{1, ready})
		if err != nil {
			return fmt.Errorf("netplay/ready event: %w", err)
		}
		err = scope.Write.Ready(
			ctx,
			model.RoomReadyPlan{
				Before: before,
				Ready:  ready,
				Evidence: model.RoomControlEvidence{
					ActorID:     actorID,
					Type:        "READY_CHANGED",
					Data:        data,
					Now:         now,
					ExpiresAtMS: now + service.waitingIdle.Milliseconds(),
				},
			},
		)
		if err != nil {
			return fmt.Errorf("netplay/set ready: %w", err)
		}
		return nil
	})
}

func (service *RoomControl) requireCurrentSelection(
	ctx context.Context,
	scope model.RoomControlScope,
	locked *model.RoomSelection,
) error {
	if locked == nil {
		return model.ErrProfileStale
	}
	current, err := service.selection(ctx, scope, locked.GameID, locked.ProfileID)
	if errors.Is(err, model.ErrInvalidProfile) {
		return model.ErrProfileStale
	}
	if err != nil {
		return err
	}
	if current != *locked {
		return model.ErrProfileStale
	}
	return nil
}
