package netplay

import (
	"context"
	"errors"
	"testing"
)

func TestLatePreparationFailureCannotAbortReplacementSession(t *testing.T) {
	t.Parallel()
	fixture := readyControlFixture(t)
	old, err := fixture.service.Start(t.Context(), fixture.room.RoomID, "host", fixture.room.Version)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.EndRoom(t.Context(), old.RoomID, "host", "USER_EXIT", nil); err != nil {
		t.Fatal(err)
	}
	room, err := fixture.service.Room(t.Context(), old.RoomID, "host")
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{"host", "guest"} {
		room, err = fixture.service.SetReady(t.Context(), room.RoomID, profile, true, room.Version)
		if err != nil {
			t.Fatal(err)
		}
	}
	replacement, err := fixture.service.Start(t.Context(), room.RoomID, "host", room.Version)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("late prepare failure")
	if err := failPreparationForSession(t.Context(), fixture.service, room.RoomID, old.CurrentSession.SessionID, sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("failure cause=%v", err)
	}
	after, err := fixture.service.Room(t.Context(), room.RoomID, "host")
	if err != nil {
		t.Fatal(err)
	}
	if after.State != RoomStateStarting || after.CurrentSession == nil || after.CurrentSession.SessionID != replacement.CurrentSession.SessionID || after.Version != replacement.Version {
		t.Fatalf("late failure aborted replacement=%+v", after)
	}
}

func failPreparationForSession(ctx context.Context, service *Service, roomID, sessionID string, cause error) error {
	return service.preparation.Fail(ctx, roomID, sessionID, cause)
}
