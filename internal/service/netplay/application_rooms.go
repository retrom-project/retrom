package netplay

import (
	"context"

	model "retrom/internal/model/netplay"
)

func (service *Service) SelectGame(
	ctx context.Context,
	roomID, actorID, gameID, profileID string,
	version int64,
) (model.Room, error) {
	room, err := service.components.Controls.SelectGame(ctx, roomID, actorID, gameID, profileID, version)
	if err != nil {
		return model.Room{}, applicationError("select game", err)
	}
	return room, nil
}

func (service *Service) ClearGame(ctx context.Context, roomID, actorID string, version int64) (model.Room, error) {
	room, err := service.components.Controls.ClearGame(ctx, roomID, actorID, version)
	if err != nil {
		return model.Room{}, applicationError("clear game", err)
	}
	return room, nil
}

func (service *Service) SetSeat(
	ctx context.Context,
	roomID, actorID string,
	playerNo int,
	version int64,
) (model.Room, error) {
	room, err := service.components.Controls.SetSeat(ctx, roomID, actorID, playerNo, version)
	if err != nil {
		return model.Room{}, applicationError("set seat", err)
	}
	return room, nil
}

func (service *Service) SetReady(
	ctx context.Context,
	roomID, actorID string,
	ready bool,
	version int64,
) (model.Room, error) {
	room, err := service.components.Controls.SetReady(ctx, roomID, actorID, ready, version)
	if err != nil {
		return model.Room{}, applicationError("set ready", err)
	}
	return room, nil
}

func (service *Service) ListRooms(
	ctx context.Context, profileID, view string, after int64, id string, limit int,
) ([]model.Room, bool, error) {
	rooms, more, err := service.components.Queries.List(ctx, profileID, view, after, id, limit)
	if err != nil {
		return nil, false, applicationError("list rooms", err)
	}
	return rooms, more, nil
}

func (service *Service) Room(ctx context.Context, id, viewer string) (model.Room, error) {
	room, err := service.components.Queries.Get(ctx, id, viewer)
	if err != nil {
		return model.Room{}, applicationError("get room", err)
	}
	return room, nil
}

func (service *Service) EndRoom(ctx context.Context, roomID, actorID, reason string, version *int64) error {
	if err := service.components.Exit.End(ctx, roomID, actorID, reason, version); err != nil {
		return applicationError("room exit", err)
	}
	return nil
}

func (service *Service) EndSystem(ctx context.Context, roomID, reason string) error {
	if err := service.components.Exit.EndSystem(ctx, roomID, reason); err != nil {
		return applicationError("room exit", err)
	}
	return nil
}

func (service *Service) Leave(ctx context.Context, roomID, actorID string, version int64) error {
	if err := service.components.Exit.Leave(ctx, roomID, actorID, version); err != nil {
		return applicationError("room exit", err)
	}
	return nil
}

func (service *Service) Kick(ctx context.Context, roomID, actorID, memberID string, version int64) error {
	if err := service.components.Exit.Kick(ctx, roomID, actorID, memberID, version); err != nil {
		return applicationError("room exit", err)
	}
	return nil
}

func (service *Service) CreateRoom(ctx context.Context, profileID string) (model.Room, error) {
	room, err := service.components.Creation.Create(ctx, profileID)
	if err != nil {
		return model.Room{}, applicationError("create room", err)
	}
	return room, nil
}

func (service *Service) Start(ctx context.Context, roomID, hostID string, version int64) (model.Room, error) {
	room, err := service.components.Starter.Start(ctx, roomID, hostID, version)
	if err != nil {
		return model.Room{}, applicationError("start", err)
	}
	return room, nil
}
