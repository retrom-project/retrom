package netplay

import (
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDraftSessionStartReturnsStateConflict(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_786_000_000_000)
	database := openNetplayTestDatabase(t.Context(), t, func() time.Time { return now })
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	seedControlHost(t, database.SQL, now)
	service := NewService(database.SQL, nil, nil, Options{MaxActiveRooms: 16, DraftIdle: time.Hour}, func() time.Time { return now })
	room, err := service.CreateRoom(t.Context(), "host")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(t.Context(), room.RoomID, "host", room.Version); !errors.Is(err, ErrRoomConflict) {
		t.Fatalf("draft start error=%v", err)
	}
}

type unavailableSessionEntropy struct{ failure error }

func (reader unavailableSessionEntropy) Read([]byte) (int, error) { return 0, reader.failure }

// This test is sequential because uuid.SetRand is package-global. Restore the reader
// before parallel tests resume, including when Start panics.
func TestSessionIdentityFailureRollsBackStart(t *testing.T) {
	fixture := newControlFixture(t)
	room, err := fixture.service.SetReady(t.Context(), fixture.room.RoomID, "host", true, fixture.room.Version)
	if err != nil {
		t.Fatal(err)
	}
	room, err = fixture.service.SetReady(t.Context(), room.RoomID, "guest", true, room.Version)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("session entropy unavailable")
	func() {
		uuid.SetRand(unavailableSessionEntropy{sentinel})
		defer uuid.SetRand(rand.Reader)
		_, err = fixture.service.Start(t.Context(), room.RoomID, "host", room.Version)
	}()
	if !errors.Is(err, sentinel) {
		t.Fatalf("failed session identity error=%v", err)
	}
	for _, query := range []string{`SELECT count(*) FROM netplay_sessions`, `SELECT count(*) FROM netplay_session_participants`} {
		var count int
		if err := fixture.database.QueryRowContext(t.Context(), query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("failed session identity retained %d rows", count)
		}
	}
	after, err := fixture.service.Room(t.Context(), room.RoomID, "host")
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != room.Version || after.State != RoomStateWaiting || after.CurrentSession != nil {
		t.Fatalf("failed start changed room=%+v", after)
	}
}
