package netplay

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	validation "retrom/internal/service/corevalidation"
)

type roomControlMemory struct {
	eligibility                                               EligibilityRepository
	bios                                                      validation.Repository
	before                                                    RoomControlSnapshot
	result                                                    Room
	readFailure, writeFailure, commitFailure, snapshotFailure error
	writes                                                    int
	seat                                                      RoomSeatPlan
	ready                                                     RoomReadyPlan
}

func (memory *roomControlMemory) CommitMutation(_ context.Context, cmd MutationCommand) (Room, error) {
	before := memory.before
	if memory.readFailure != nil {
		return Room{}, memory.readFailure
	}
	if cmd.HostOnly && before.HostID != cmd.ActorID {
		return Room{}, ErrForbidden
	}
	if before.Version != cmd.Version {
		return Room{}, ErrPrecondition
	}
	found := false
	for _, s := range cmd.States {
		if s == before.State {
			found = true
			break
		}
	}
	if !found {
		return Room{}, ErrRoomConflict
	}
	scope := RoomControlScope{Read: memory, Write: memory, Eligibility: memory.eligibility, BIOS: memory.bios}
	if err := cmd.Apply(scope, before, cmd.NowMS); err != nil {
		return Room{}, err
	}
	if memory.snapshotFailure != nil {
		return Room{}, memory.snapshotFailure
	}
	if memory.commitFailure != nil {
		return Room{}, memory.commitFailure
	}
	return memory.result, nil
}

func (memory *roomControlMemory) Current(context.Context, string, string) (RoomControlSnapshot, error) {
	return memory.before, memory.readFailure
}

func (memory *roomControlMemory) Snapshot(context.Context, string) (Room, error) {
	return memory.result, memory.snapshotFailure
}

func (memory *roomControlMemory) Select(context.Context, RoomSelectionPlan) error {
	memory.writes++
	return memory.writeFailure
}

func (memory *roomControlMemory) Clear(context.Context, RoomClearPlan) error {
	memory.writes++
	return memory.writeFailure
}

func (memory *roomControlMemory) Seat(_ context.Context, plan RoomSeatPlan) error {
	memory.writes++
	memory.seat = plan
	return memory.writeFailure
}

func (memory *roomControlMemory) Ready(_ context.Context, plan RoomReadyPlan) error {
	memory.writes++
	memory.ready = plan
	return memory.writeFailure
}

func controlSnapshot() RoomControlSnapshot {
	return RoomControlSnapshot{RoomID: "room", HostID: "host", State: RoomStateWaiting, Version: 4, Selection: &RoomSelection{MaxPlayers: 2}, Member: &SeatMember{ID: "guest-member", ProfileID: "guest", Role: "GUEST", PlayerNo: 2, Version: 3}}
}

func TestRoomControlsValidateBeforeWriting(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, action string
		change       func(*RoomControlSnapshot)
		want         error
	}{
		{"draft seat", "seat", func(before *RoomControlSnapshot) { before.State = RoomStateDraft; before.Selection = nil }, ErrRoomConflict},
		{"draft ready", "ready", func(before *RoomControlSnapshot) { before.State = RoomStateDraft; before.Selection = nil }, ErrRoomConflict},
		{"stale seat", "seat", func(before *RoomControlSnapshot) { before.Version = 5 }, ErrPrecondition},
		{"outsider ready", "ready", func(before *RoomControlSnapshot) { before.Member = nil }, ErrForbidden},
		{"departed ready", "ready", func(before *RoomControlSnapshot) { at := int64(1); before.Member.LeftAtMS = &at }, ErrForbidden},
		{"host seat", "seat", func(before *RoomControlSnapshot) { before.Member.Role = "HOST" }, ErrForbidden},
		{"ready member seat", "seat", func(before *RoomControlSnapshot) { before.Member.Ready = true }, ErrRoomConflict},
		{"seat outside profile", "seat", func(before *RoomControlSnapshot) { before.Selection.MaxPlayers = 1 }, ErrInvalidSeat},
		{"occupied seat", "seat", func(before *RoomControlSnapshot) { before.Occupants = []SeatMember{{ProfileID: "other", PlayerNo: 2}} }, ErrSeatTaken},
		{"nonhost clear", "clear", func(*RoomControlSnapshot) {}, ErrForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := controlSnapshot()
			test.change(&before)
			memory := &roomControlMemory{before: before}
			service := NewRoomControl(memory, nil, time.Hour, time.Hour, time.Now)
			var err error
			switch test.action {
			case "seat":
				_, err = service.SetSeat(t.Context(), "room", "guest", 2, 4)
			case "ready":
				_, err = service.SetReady(t.Context(), "room", "guest", false, 4)
			case "clear":
				_, err = service.ClearGame(t.Context(), "room", "guest", 4)
			}
			if !errors.Is(err, test.want) || memory.writes != 0 {
				t.Fatalf("error=%v writes=%d", err, memory.writes)
			}
		})
	}
}

func TestSeatPlanDistinguishesJoinAndMove(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_786_000_000_000)
	for _, existing := range []bool{false, true} {
		before := controlSnapshot()
		if !existing {
			before.Member = nil
		}
		memory := &roomControlMemory{before: before, result: Room{RoomID: "room"}}
		service := NewRoomControl(memory, nil, time.Minute, time.Hour, func() time.Time { return now })
		service.newID = func() (string, error) { return "new-member", nil }
		if _, err := service.SetSeat(t.Context(), "room", "guest", 2, 4); err != nil {
			t.Fatal(err)
		}
		plan := memory.seat
		assertSeatPlan(t, plan, existing, now)
	}
}

func TestRoomControlFailureDoesNotReturnSuccessfulProjection(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("storage failed")
	for _, memory := range []*roomControlMemory{{readFailure: sentinel}, {writeFailure: sentinel}, {snapshotFailure: sentinel}, {commitFailure: sentinel}} {
		memory.before = controlSnapshot()
		memory.result = Room{RoomID: "room"}
		service := NewRoomControl(memory, nil, time.Hour, time.Hour, time.Now)
		room, err := service.SetReady(t.Context(), "room", "guest", false, 4)
		if !errors.Is(err, sentinel) || room.RoomID != "" {
			t.Fatalf("failure room=%+v error=%v", room, err)
		}
	}
}

func TestNewSeatIdentityFailureDoesNotWrite(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("entropy failed")
	memory := &roomControlMemory{before: controlSnapshot()}
	memory.before.Member = nil
	service := NewRoomControl(memory, nil, time.Hour, time.Hour, time.Now)
	service.newID = func() (string, error) { return "", sentinel }
	_, err := service.SetSeat(t.Context(), "room", "guest", 2, 4)
	if !errors.Is(err, sentinel) || memory.writes != 0 {
		t.Fatalf("identity error=%v writes=%d", err, memory.writes)
	}
}

func TestHostCannotClaimGuestSeatThroughTheService(t *testing.T) {
	t.Parallel()
	before := controlSnapshot()
	before.Member.Role = "HOST"
	if err := validateSeat(before, 2, "host"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("host seat mutation error=%v", err)
	}
}

func assertSeatPlan(t *testing.T, plan RoomSeatPlan, existing bool, now time.Time) {
	t.Helper()
	var event struct {
		SchemaVersion int  `json:"schemaVersion"`
		ToPlayerNo    int  `json:"toPlayerNo"`
		FromPlayerNo  *int `json:"fromPlayerNo"`
	}
	if err := json.Unmarshal(plan.Evidence.Data, &event); err != nil {
		t.Fatal(err)
	}
	if event.SchemaVersion != 1 || event.ToPlayerNo != 2 || plan.Evidence.ExpiresAtMS != now.Add(time.Hour).UnixMilli() {
		t.Fatalf("seat plan=%+v event=%+v", plan, event)
	}
	if existing {
		if plan.MemberID != "guest-member" || plan.Evidence.Type != "SEAT_CHANGED" || event.FromPlayerNo == nil || *event.FromPlayerNo != 2 {
			t.Fatalf("move plan=%+v event=%+v", plan, event)
		}
	} else if plan.MemberID != "new-member" || plan.Evidence.Type != "MEMBER_JOINED" || event.FromPlayerNo != nil {
		t.Fatalf("join plan=%+v event=%+v", plan, event)
	}
}
