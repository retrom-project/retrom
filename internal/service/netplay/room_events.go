package netplay

import (
	"context"
	"fmt"

	model "retrom/internal/model/netplay"
)

type RoomEvents struct{ repository model.RoomEventsRepository }

func NewRoomEvents(repository model.RoomEventsRepository) *RoomEvents {
	return &RoomEvents{repository}
}

func (service *RoomEvents) Events(ctx context.Context, roomID string, after int64, limit int) ([]model.Event, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	if after < 0 {
		after = 0
	}
	page, err := service.repository.Page(ctx, roomID, after, limit)
	if err != nil {
		return nil, fmt.Errorf("netplay/read events: %w", err)
	}
	if !page.Exists {
		return nil, model.ErrRoomNotFound
	}
	return page.Events, nil
}
