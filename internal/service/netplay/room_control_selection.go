package netplay

import (
	"context"
	"fmt"

	model "retrom/internal/model/netplay"
)

func (service *RoomControl) SelectGame(
	ctx context.Context,
	roomID, actorID, gameID, profileID string,
	version int64,
) (model.Room, error) {
	selection, err := service.selection(ctx, gameID, profileID)
	if err != nil {
		return model.Room{}, err
	}
	now := service.now().UnixMilli()
	result, err := service.repository.CommitSelectGame(ctx, model.SelectGameCommand{
		RoomID:    roomID,
		ActorID:   actorID,
		Version:   version,
		Selection: selection,
		NowMS:     now,
		IdleMS:    service.waitingIdle.Milliseconds(),
	})
	if err != nil {
		return model.Room{}, fmt.Errorf("netplay/select game: %w", err)
	}
	return roomForViewer(result, actorID, now), nil
}

func (service *RoomControl) ClearGame(
	ctx context.Context,
	roomID, actorID string,
	version int64,
) (model.Room, error) {
	now := service.now().UnixMilli()
	result, err := service.repository.CommitClearGame(ctx, model.ClearGameCommand{
		RoomID:  roomID,
		ActorID: actorID,
		Version: version,
		NowMS:   now,
		IdleMS:  service.draftIdle.Milliseconds(),
	})
	if err != nil {
		return model.Room{}, fmt.Errorf("netplay/clear game: %w", err)
	}
	return roomForViewer(result, actorID, now), nil
}
