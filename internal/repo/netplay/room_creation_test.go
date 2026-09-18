package netplay_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	service "retrom/internal/model/netplay"
	repository "retrom/internal/repo/netplay"
	"retrom/internal/repo/store"
)

func TestRoomCreationRollsBackRoomHostAndEventTogether(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_786_000_000_000)
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := database.SQL.ExecContext(t.Context(), `INSERT INTO profiles(id,display_name,created_at_ms)VALUES('host','Host',?)`, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("late failure")
	repo := repository.NewRoomCreation(database.SQL)
	repo.WithPreCommitHook(func() error { return sentinel })
	room, err := repo.CommitRoomCreation(t.Context(), service.RoomCreationCommand{
		Plan: service.RoomCreationPlan{
			RoomID: "room", MemberID: "member", HostID: "host",
			Now: now.UnixMilli(), ExpiresAtMS: now.Add(time.Hour).UnixMilli(),
			Event: []byte(`{"schemaVersion":1}`),
		},
		Maximum: 16,
	})
	if !errors.Is(err, sentinel) || room.RoomID != "" {
		t.Fatalf("late failure=%v room=%+v", err, room)
	}
	for _, query := range []string{`SELECT count(*) FROM netplay_rooms`, `SELECT count(*) FROM netplay_room_members`, `SELECT count(*) FROM netplay_events`} {
		var count int
		if err := database.SQL.QueryRowContext(t.Context(), query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("late failure retained %d records for %s", count, query)
		}
	}
}
