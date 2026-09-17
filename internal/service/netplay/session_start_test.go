package netplay

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	validation "retrom/internal/model/corevalidation"
	model "retrom/internal/model/netplay"
)

type sessionStartMemory struct {
	room                                        *roomControlMemory
	eligibility                                 model.EligibilityRepository
	bios                                        validation.Repository
	number                                      int
	plan                                        model.SessionStartPlan
	inserts                                     int
	numberFailure, insertFailure, commitFailure error
}

func (memory *sessionStartMemory) WithStart(_ context.Context, work func(model.SessionStartScope) error) error {
	err := work(model.SessionStartScope{Read: memory.room, Write: memory, Eligibility: memory.eligibility, BIOS: memory.bios})
	if err != nil {
		return err
	}
	return memory.commitFailure
}

func (memory *sessionStartMemory) NextNumber(context.Context, string) (int, error) {
	return memory.number, memory.numberFailure
}

func (memory *sessionStartMemory) Insert(_ context.Context, plan model.SessionStartPlan) (model.Room, error) {
	memory.plan = plan
	memory.inserts++
	return memory.room.result, memory.insertFailure
}

func sessionStartFixture(t *testing.T) (*SessionStart, *sessionStartMemory, *eligibilityMemory) {
	t.Helper()
	control, room, eligibility := controlSelectionFixture(t)
	room.before.Occupants = []model.SeatMember{
		{ID: "host-member", ProfileID: "host", Role: "HOST", PlayerNo: 1, Ready: true},
		{ID: "guest-member", ProfileID: "guest", Role: "GUEST", PlayerNo: 2, Ready: true},
	}
	room.result = model.Room{
		RoomID: "room", State: model.RoomStateStarting, Version: 5,
		CurrentSession: &model.SessionSummary{
			SessionID: "session", SessionNo: 2, State: "PREPARING",
		},
	}
	memory := &sessionStartMemory{
		room:        room,
		eligibility: eligibility,
		bios:        controlBIOSRepository{},
		number:      2,
	}
	service := NewSessionStart(memory, control.registry, func() time.Time { return time.UnixMilli(1_786_000_000_000) })
	service.newID = func() (string, error) { return "session", nil }
	return service, memory, eligibility
}

func TestSessionStartFreezesParticipantsAndProfile(t *testing.T) {
	t.Parallel()
	service, memory, _ := sessionStartFixture(t)
	room, err := service.Start(t.Context(), "room", "host", 4)
	if err != nil || room.CurrentSession == nil || room.CurrentSession.SessionID != "session" || memory.inserts != 1 {
		t.Fatalf("started room=%+v writes=%d error=%v", room, memory.inserts, err)
	}
	plan := memory.plan
	if plan.SessionID != "session" || plan.SessionNo != 2 || plan.SeatMask != 3 || len(plan.Members) != 2 || plan.Profile.Selection != *memory.room.before.Selection || !json.Valid(plan.Profile.Canonical) {
		t.Fatalf("frozen start plan=%+v", plan)
	}
	var event struct{ SchemaVersion, PlayerCount, OccupiedSeatMask int }
	if err := json.Unmarshal(plan.Event, &event); err != nil {
		t.Fatal(err)
	}
	if event.SchemaVersion != 1 || event.PlayerCount != 2 || event.OccupiedSeatMask != 3 {
		t.Fatalf("start event=%+v", event)
	}
}

func TestSessionStartDoesNotPublishFailedWrites(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("start failed")
	for _, phase := range []string{"read", "number", "identity", "insert", "commit"} {
		t.Run(phase, func(t *testing.T) {
			service, memory, _ := sessionStartFixture(t)
			switch phase {
			case "read":
				memory.room.readFailure = sentinel
			case "number":
				memory.numberFailure = sentinel
			case "identity":
				service.newID = func() (string, error) { return "", sentinel }
			case "insert":
				memory.insertFailure = sentinel
			case "commit":
				memory.commitFailure = sentinel
			}
			room, err := service.Start(t.Context(), "room", "host", 4)
			if !errors.Is(err, sentinel) || room.RoomID != "" || room.CurrentSession != nil {
				t.Fatalf("failed start room=%+v error=%v", room, err)
			}
			if phase == "identity" && memory.inserts != 0 {
				t.Fatalf("identity failure inserted %d sessions", memory.inserts)
			}
		})
	}
}

func TestSessionStartRevalidatesLockedContentInTransaction(t *testing.T) {
	t.Parallel()
	service, memory, eligibility := sessionStartFixture(t)
	eligibility.rows["game"][0].SourceManifestDigest = strings.Repeat("c", 64)
	_, err := service.Start(t.Context(), "room", "host", 4)
	if !errors.Is(err, model.ErrProfileStale) || memory.inserts != 0 {
		t.Fatalf("changed content error=%v writes=%d", err, memory.inserts)
	}
}

func TestSessionStartRequiresHostVersionAndWaitingState(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		actor, state string
		version      int64
		want         error
	}{{"guest", model.RoomStateWaiting, 4, model.ErrForbidden}, {"host", model.RoomStateWaiting, 3, model.ErrPrecondition}, {"host", model.RoomStateDraft, 4, model.ErrRoomConflict}} {
		service, memory, _ := sessionStartFixture(t)
		memory.room.before.State = test.state
		_, err := service.Start(t.Context(), "room", test.actor, test.version)
		if !errors.Is(err, test.want) || memory.inserts != 0 {
			t.Fatalf("start error=%v writes=%d", err, memory.inserts)
		}
	}
}

func TestSessionStartSeatMaskRequiresReadyHostAndUniqueBoundedSeats(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name               string
		players            []int
		unready, guestHost bool
		want               int
	}{
		{name: "two", players: []int{1, 2}, want: 3},
		{name: "three", players: []int{1, 2, 3}, want: 7},
		{name: "four", players: []int{1, 2, 3, 4}, want: 15},
		{name: "empty seat", players: []int{1, 3}, want: 5},
		{name: "alone", players: []int{1}},
		{name: "missing host", players: []int{2, 3}},
		{name: "duplicate", players: []int{1, 2, 2}},
		{name: "out of range", players: []int{1, 5}},
		{name: "unready", players: []int{1, 2}, unready: true},
		{name: "wrong host", players: []int{1, 2}, guestHost: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := model.RoomControlSnapshot{HostID: "host", Selection: &model.RoomSelection{MaxPlayers: 4}}
			for _, player := range test.players {
				member := model.SeatMember{ProfileID: "guest", Role: "GUEST", PlayerNo: player, Ready: !test.unready}
				if player == 1 && !test.guestHost {
					member.Role = "HOST"
					member.ProfileID = "host"
				}
				before.Occupants = append(before.Occupants, member)
			}
			mask, err := startSeatMask(before)
			if test.want == 0 {
				if !errors.Is(err, model.ErrRoomNotReady) || mask != 0 {
					t.Fatalf("invalid seats mask=%d error=%v", mask, err)
				}
			} else if err != nil || mask != test.want {
				t.Fatalf("seat mask=%d want=%d error=%v", mask, test.want, err)
			}
		})
	}
}
