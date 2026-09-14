package netplay

import (
	"context"
	"fmt"
)

type RoomEvents struct{ repository RoomEventsRepository }

func NewRoomEvents(repository RoomEventsRepository) *RoomEvents { return &RoomEvents{repository} }
func (service *RoomEvents) Events(ctx context.Context, roomID string, after int64, limit int) ([]Event, error) {
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
		return nil, ErrRoomNotFound
	}
	return page.Events, nil
}
