package netplay

import (
	"context"

	repository "retrom/internal/persistence/netplay"
	application "retrom/internal/service/netplay"
)

type (
	RoomMember      = application.RoomMember
	RoomGame        = application.RoomGame
	SessionSummary  = application.SessionSummary
	RoomPermissions = application.RoomPermissions
	Room            = application.Room
)

func (service *Service) roomControl() *application.RoomControl {
	return application.NewRoomControl(
		repository.NewRoomControl(service.database),
		service.registry,
		service.options.DraftIdle,
		service.options.WaitingIdle,
		service.clock.Now,
	)
}

func (service *Service) SelectGame(
	ctx context.Context,
	roomID, actorID, gameID, profileID string,
	version int64,
) (Room, error) {
	room, err := service.roomControl().SelectGame(ctx, roomID, actorID, gameID, profileID, version)
	if err != nil {
		return Room{}, serviceError("select game", err)
	}
	return room, nil
}

func (service *Service) ClearGame(ctx context.Context, roomID, actorID string, version int64) (Room, error) {
	room, err := service.roomControl().ClearGame(ctx, roomID, actorID, version)
	if err != nil {
		return Room{}, serviceError("clear game", err)
	}
	return room, nil
}

func (service *Service) SetSeat(
	ctx context.Context,
	roomID, actorID string,
	playerNo int,
	version int64,
) (Room, error) {
	room, err := service.roomControl().SetSeat(ctx, roomID, actorID, playerNo, version)
	if err != nil {
		return Room{}, serviceError("set seat", err)
	}
	return room, nil
}

func (service *Service) SetReady(ctx context.Context, roomID, actorID string, ready bool, version int64) (Room, error) {
	room, err := service.roomControl().SetReady(ctx, roomID, actorID, ready, version)
	if err != nil {
		return Room{}, serviceError("set ready", err)
	}
	return room, nil
}
