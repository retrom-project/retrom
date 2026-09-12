package netplay

import "testing"

func TestRoomEventsNormalizeNonPositiveLimit(t *testing.T) {
	t.Parallel()
	fixture := newControlFixture(t)
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Errorf("non-positive event limit panicked: %v", recovered)
		}
	}()
	events, err := fixture.service.Events(t.Context(), fixture.room.RoomID, 0, -1)
	if err != nil || len(events) == 0 {
		t.Fatalf("events=%v error=%v", events, err)
	}
}
