package netplay

import (
	"errors"
	"testing"
)

func TestGuestCannotCloseWholeRoom(t *testing.T) {
	t.Parallel()
	fixture := newControlFixture(t)
	beforeVersion, beforeEvents := controlCounts(t, fixture)
	err := fixture.service.EndRoom(t.Context(), fixture.room.RoomID, "guest", "HOST_CLOSED", &fixture.room.Version)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("guest whole-room close error=%v", err)
	}
	after, err := fixture.service.Room(t.Context(), fixture.room.RoomID, "host")
	if err != nil {
		t.Fatal(err)
	}
	version, events := controlCounts(t, fixture)
	if after.State != RoomStateWaiting || after.Version != fixture.room.Version || version != beforeVersion || events != beforeEvents {
		t.Fatalf("forbidden room close changed room=%+v memberVersions=%d events=%d", after, version, events)
	}
}

func TestDraftCannotReturnToWaitingWithoutSelection(t *testing.T) {
	t.Parallel()
	fixture := newControlFixture(t)
	if err := fixture.service.Kick(t.Context(), fixture.room.RoomID, "host", fixture.room.Members[1].MemberID, fixture.room.Version); err != nil {
		t.Fatal(err)
	}
	room, err := fixture.service.Room(t.Context(), fixture.room.RoomID, "host")
	if err != nil {
		t.Fatal(err)
	}
	room, err = fixture.service.ClearGame(t.Context(), room.RoomID, "host", room.Version)
	if err != nil {
		t.Fatal(err)
	}
	err = fixture.service.EndRoom(t.Context(), room.RoomID, "host", "USER_EXIT", nil)
	if !errors.Is(err, ErrRoomConflict) {
		t.Fatalf("draft session end error=%v", err)
	}
}
