package netplay

import (
	"context"

	repository "retrom/internal/persistence/netplay"
	application "retrom/internal/service/netplay"
)

func (service *Service) Start(ctx context.Context, roomID, hostID string, version int64) (Room, error) {
	starter := application.NewSessionStart(
		repository.NewSessionStart(service.database),
		service.registry,
		service.clock.Now,
	)
	room, err := starter.Start(ctx, roomID, hostID, version)
	if err != nil {
		return Room{}, serviceError("start", err)
	}
	return room, nil
}
