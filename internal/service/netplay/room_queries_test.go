package netplay

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type roomQueryMemory struct {
	rooms        map[string]Room
	ids          []string
	filter       RoomFilter
	lists, reads int
	failure      error
}

func (memory *roomQueryMemory) RoomIDs(_ context.Context, filter RoomFilter) ([]string, error) {
	memory.lists++
	memory.filter = filter
	return memory.ids, memory.failure
}

func (memory *roomQueryMemory) Snapshot(_ context.Context, id string) (Room, error) {
	memory.reads++
	if memory.failure != nil {
		return Room{}, memory.failure
	}
	room, ok := memory.rooms[id]
	if !ok {
		return Room{}, ErrRoomNotFound
	}
	return room, nil
}

func TestRoomPermissionsComeFromCurrentViewerAndState(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_786_000_000_000)
	for _, test := range []struct {
		name, state, viewer string
		want                RoomPermissions
		self                string
	}{
		{"host draft", "DRAFT", "private-host", RoomPermissions{Host: true, Member: true, CanSelect: true, CanClose: true}, "host-member"},
		{"host waiting", "WAITING", "private-host", RoomPermissions{Host: true, Member: true, CanSelect: true, CanReady: true, CanStart: true, CanClose: true}, "host-member"},
		{"guest waiting", "WAITING", "private-guest", RoomPermissions{Member: true, CanReady: true}, "guest-member"},
		{"visitor waiting", "WAITING", "visitor", RoomPermissions{CanJoin: true}, ""},
		{"host running", "RUNNING", "private-host", RoomPermissions{Host: true, Member: true, CanClose: true}, "host-member"},
		{"host ended", "ENDED", "private-host", RoomPermissions{Host: true, Member: true}, "host-member"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stored := Room{RoomID: "room", State: test.state, Members: []RoomMember{{MemberID: "host-member", ProfileID: "private-host", Role: "HOST", Ready: true}, {MemberID: "guest-member", ProfileID: "private-guest", Role: "GUEST", Ready: true}}}
			memory := &roomQueryMemory{rooms: map[string]Room{"room": stored}}
			room, err := NewRoomQueries(memory, func() time.Time { return now }).Get(t.Context(), "room", test.viewer)
			if err != nil || room.Permissions != test.want || room.ServerNowMS != now.UnixMilli() {
				t.Fatalf("room=%+v error=%v", room, err)
			}
			if test.self == "" {
				if room.SelfMemberID != nil {
					t.Fatalf("visitor self=%v", room.SelfMemberID)
				}
			} else if room.SelfMemberID == nil || *room.SelfMemberID != test.self {
				t.Fatalf("member self=%v", room.SelfMemberID)
			}
			data, err := json.Marshal(room)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "private-host") || strings.Contains(string(data), "private-guest") {
				t.Fatalf("private identities leaked: %s", data)
			}
			if memory.rooms["room"].Permissions.Member {
				t.Fatal("repository snapshot mutated")
			}
		})
	}
}

func TestRoomListBoundsProjectionAndRecentWindow(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_786_000_000_000)
	memory := &roomQueryMemory{ids: []string{"first", "extra"}, rooms: map[string]Room{"first": {RoomID: "first", Members: []RoomMember{}}}}
	result, more, err := NewRoomQueries(memory, func() time.Time { return now }).List(t.Context(), "viewer", "recent", 88, "cursor", 1)
	if err != nil || !more || len(result) != 1 || memory.reads != 1 {
		t.Fatalf("rooms=%+v more=%v reads=%d error=%v", result, more, memory.reads, err)
	}
	want := RoomFilter{ProfileID: "viewer", View: "recent", AfterUpdatedAtMS: 88, AfterRoomID: "cursor", Limit: 2, RecentSinceMS: now.Add(-24 * time.Hour).UnixMilli()}
	if memory.filter != want {
		t.Fatalf("filter=%+v want=%+v", memory.filter, want)
	}
}

func TestRoomQueryFailuresRemainDistinctFromMissingRoom(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("read failed")
	memory := &roomQueryMemory{}
	service := NewRoomQueries(memory, time.Now)
	if _, err := service.Get(t.Context(), "missing", "viewer"); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("missing room=%v", err)
	}
	memory.failure = sentinel
	if _, err := service.Get(t.Context(), "room", "viewer"); !errors.Is(err, sentinel) || errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("storage failure=%v", err)
	}
	if _, _, err := service.List(t.Context(), "viewer", "active", 0, "", 24); !errors.Is(err, sentinel) {
		t.Fatalf("list failure=%v", err)
	}
}

func TestInvalidRoomPageDoesNotReadRepository(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		view  string
		limit int
	}{{"unknown", 1}, {"active", 0}, {"recent", 101}} {
		memory := &roomQueryMemory{}
		_, _, err := NewRoomQueries(memory, time.Now).List(t.Context(), "viewer", test.view, 0, "", test.limit)
		if !errors.Is(err, ErrRoomConflict) || memory.lists != 0 {
			t.Fatalf("invalid page error=%v reads=%d", err, memory.lists)
		}
	}
}
