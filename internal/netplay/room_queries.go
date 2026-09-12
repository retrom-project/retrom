package netplay

import (
	"context"

	repository "retrom/internal/persistence/netplay"
	application "retrom/internal/service/netplay"
)

func (service *Service) roomQueries() *application.RoomQueries {
	return application.NewRoomQueries(repository.NewRoomQueries(service.database), service.clock.Now)
}

func (service *Service) ListRooms(
	ctx context.Context, profileID, view string, after int64, id string, limit int,
) ([]Room, bool, error) {
	rooms, more, err := service.roomQueries().List(ctx, profileID, view, after, id, limit)
	if err != nil {
		return nil, false, serviceError("list rooms", err)
	}
	return rooms, more, nil
}

func (service *Service) Room(ctx context.Context, id, viewer string) (Room, error) {
	room, err := service.roomQueries().Get(ctx, id, viewer)
	if err != nil {
		return Room{}, serviceError("get room", err)
	}
	return room, nil
}
