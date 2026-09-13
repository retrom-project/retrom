package netplay

import (
	"context"
	"testing"
	"time"
)

func TestLeftMemberDoesNotSeeRoomInActiveList(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.UnixMilli(1_786_000_000_000)
	database := openNetplayTestDatabase(ctx, t, func() time.Time { return now })
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, id := range []string{"host", "guest"} {
		if _, err := database.SQL.ExecContext(ctx, `INSERT INTO profiles(id,display_name,created_at_ms)VALUES(?,?,?)`, id, id, now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(database.SQL, nil, nil, Options{MaxActiveRooms: 16, DraftIdle: time.Hour}, func() time.Time { return now })
	room, err := service.CreateRoom(ctx, "host")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `INSERT INTO netplay_room_members(id,room_id,profile_id,role,player_no,ready,version,joined_at_ms,updated_at_ms,left_at_ms,leave_reason)VALUES('departed',?,'guest','GUEST',2,0,1,?,?,?,'USER_LEFT')`, room.RoomID, now.UnixMilli(), now.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	rooms, _, err := service.ListRooms(ctx, "guest", "active", 0, "", 24)
	if err != nil || len(rooms) != 0 {
		t.Fatalf("departed guest active rooms=%+v error=%v", rooms, err)
	}
	rooms, _, err = service.ListRooms(ctx, "host", "active", 0, "", 24)
	if err != nil || len(rooms) != 1 {
		t.Fatalf("host active rooms=%+v error=%v", rooms, err)
	}

	now = now.Add(time.Hour)
	if err := service.ExpireRooms(ctx); err != nil {
		t.Fatal(err)
	}
	rooms, _, err = service.ListRooms(ctx, "guest", "recent", 0, "", 24)
	if err != nil || len(rooms) != 1 {
		t.Fatalf("departed guest recent rooms=%+v error=%v", rooms, err)
	}
	now = now.Add(25 * time.Hour)
	rooms, _, err = service.ListRooms(ctx, "guest", "recent", 0, "", 24)
	if err != nil || len(rooms) != 0 {
		t.Fatalf("expired recent rooms=%+v error=%v", rooms, err)
	}
}
