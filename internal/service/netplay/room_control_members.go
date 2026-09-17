package netplay

import (
	"context"
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
	memberID, err := service.newID()
	if err != nil {
		return model.Room{}, fmt.Errorf("netplay/new member identity: %w", err)
	}
	now := service.now().UnixMilli()
	result, err := service.repository.CommitSetSeat(ctx, model.SetSeatCommand{
		RoomID:      roomID,
		ActorID:     actorID,
		Version:     version,
		PlayerNo:    playerNo,
		NewMemberID: memberID,
		NowMS:       now,
		IdleMS:      service.waitingIdle.Milliseconds(),
	})
	if err != nil {
		return model.Room{}, fmt.Errorf("netplay/set seat: %w", err)
	}
	return roomForViewer(result, actorID, now), nil
}

func (service *RoomControl) SetReady(
	ctx context.Context,
	roomID, actorID string,
	ready bool,
	version int64,
) (model.Room, error) {
	if ready {
		snapshot, err := service.repository.LoadControlSnapshot(ctx, roomID, actorID)
		if err != nil {
			return model.Room{}, fmt.Errorf("netplay/read ready state: %w", err)
		}
		if err := service.requireCurrentSelection(ctx, snapshot.Selection); err != nil {
			return model.Room{}, err
		}
	}
	now := service.now().UnixMilli()
	result, err := service.repository.CommitSetReady(ctx, model.SetReadyCommand{
		RoomID:  roomID,
		ActorID: actorID,
		Version: version,
		Ready:   ready,
		NowMS:   now,
		IdleMS:  service.waitingIdle.Milliseconds(),
	})
	if err != nil {
		return model.Room{}, fmt.Errorf("netplay/set ready: %w", err)
	}
	return roomForViewer(result, actorID, now), nil
}
