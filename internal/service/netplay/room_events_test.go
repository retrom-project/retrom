package netplay

import (
	"context"
	"errors"
	model "retrom/internal/model/netplay"
	"testing"
)

type roomEventsMemory struct {
	page    model.RoomEventPage
	failure error
	limit   int
	after   int64
}

func (memory *roomEventsMemory) Page(_ context.Context, _ string, after int64, limit int) (model.RoomEventPage, error) {
	memory.after = after
	memory.limit = limit
	return memory.page, memory.failure
}

func TestRoomEventsBoundsMissingRoomsAndFailures(t *testing.T) {
	t.Parallel()
	memory := &roomEventsMemory{page: model.RoomEventPage{Exists: true, Events: []model.Event{{ID: 9}}}}
	service := NewRoomEvents(memory)
	for _, limit := range []int{-1, 0, 100, 1000} {
		events, err := service.Events(t.Context(), "room", 8, limit)
		if err != nil || len(events) != 1 || memory.limit != 100 || memory.after != 8 {
			t.Fatalf("events=%v limit=%d error=%v", events, memory.limit, err)
		}
	}
	memory.page.Exists = false
	if _, err := service.Events(t.Context(), "missing", 0, 10); !errors.Is(err, model.ErrRoomNotFound) {
		t.Fatalf("missing room=%v", err)
	}
	sentinel := errors.New("read failure")
	memory.failure = sentinel
	if events, err := service.Events(t.Context(), "room", 0, 10); !errors.Is(err, sentinel) || events != nil {
		t.Fatalf("failed events=%v error=%v", events, err)
	}
}
