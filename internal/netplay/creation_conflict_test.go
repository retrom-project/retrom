package netplay

import (
	"errors"
	"testing"
	"time"
)

func TestDuplicateHostReturnsRoomConflictBeforeCapacity(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_786_000_000_000)
	database := openNetplayTestDatabase(t.Context(), t, func() time.Time { return now })
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := database.SQL.ExecContext(t.Context(), `INSERT INTO profiles(id,display_name,created_at_ms)VALUES('host','Host',?)`, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	service := NewService(database.SQL, nil, nil, Options{MaxActiveRooms: 16, DraftIdle: time.Hour}, func() time.Time { return now })
	if _, err := service.CreateRoom(t.Context(), "host"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateRoom(t.Context(), "host"); !errors.Is(err, ErrRoomConflict) {
		t.Fatalf("duplicate active host error=%v", err)
	}
	var rooms, members, events int
	for query, target := range map[string]*int{`SELECT count(*) FROM netplay_rooms`: &rooms, `SELECT count(*) FROM netplay_room_members`: &members, `SELECT count(*) FROM netplay_events`: &events} {
		if err := database.SQL.QueryRowContext(t.Context(), query).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if rooms != 1 || members != 1 || events != 1 {
		t.Fatalf("duplicate created records: rooms=%d members=%d events=%d", rooms, members, events)
	}
}
