package netplay

import (
	"context"

	repository "retrom/internal/persistence/netplay"
	application "retrom/internal/service/netplay"
)

func (service *Service) roomMaintenance() *application.RoomMaintenance {
	return application.NewRoomMaintenance(
		repository.NewRoomMaintenance(service.database),
		service.roomExit(),
		service.clock.Now,
	)
}

func (service *Service) ExpireRooms(ctx context.Context) error {
	if err := service.roomMaintenance().Expire(ctx); err != nil {
		return serviceError("expire rooms", err)
	}
	return nil
}

func (service *Service) Recover(ctx context.Context, reason string) error {
	if err := service.roomMaintenance().Recover(ctx, reason); err != nil {
		return serviceError("recover rooms", err)
	}
	return nil
}

func (service *Service) Events(ctx context.Context, roomID string, after int64, limit int) ([]Event, error) {
	events, err := application.NewRoomEvents(repository.NewRoomEvents(service.database)).Events(ctx, roomID, after, limit)
	if err != nil {
		return nil, serviceError("read events", err)
	}
	return events, nil
}
