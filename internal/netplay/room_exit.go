package netplay

import (
	"context"

	repository "retrom/internal/persistence/netplay"
	application "retrom/internal/service/netplay"
)

func (service *Service) roomExit() *application.RoomExit {
	return application.NewRoomExit(
		repository.NewRoomExit(service.database),
		service.options.WaitingIdle,
		service.clock.Now,
	)
}

func (service *Service) EndRoom(ctx context.Context, roomID, actorID, reason string, version *int64) error {
	if err := service.roomExit().End(ctx, roomID, actorID, reason, version); err != nil {
		return serviceError("room exit", err)
	}
	return nil
}

func (service *Service) endRoomSystem(ctx context.Context, roomID, reason string) error {
	if err := service.roomExit().EndSystem(ctx, roomID, reason); err != nil {
		return serviceError("room exit", err)
	}
	return nil
}

func (service *Service) Leave(ctx context.Context, roomID, actorID string, version int64) error {
	if err := service.roomExit().Leave(ctx, roomID, actorID, version); err != nil {
		return serviceError("room exit", err)
	}
	return nil
}

func (service *Service) Kick(ctx context.Context, roomID, actorID, memberID string, version int64) error {
	if err := service.roomExit().Kick(ctx, roomID, actorID, memberID, version); err != nil {
		return serviceError("room exit", err)
	}
	return nil
}
