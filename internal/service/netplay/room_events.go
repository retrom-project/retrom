package netplay

import (
	"context"
	"fmt"
)

type Event struct {
	ID        int64          `json:"id"`
	EventType string         `json:"eventType"`
	Data      map[string]any `json:"data"`
	CreatedAt int64          `json:"createdAtMs"`
}
type RoomEventPage struct {
	Exists bool
	Events []Event
}
type RoomEventsRepository interface {
	Page(context.Context, string, int64, int) (RoomEventPage, error)
}
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
