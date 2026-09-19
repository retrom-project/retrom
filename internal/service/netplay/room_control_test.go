package netplay

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	corevalidationmodel "retrom/internal/model/corevalidation"
	model "retrom/internal/model/netplay"
)

type roomControlMemory struct {
	eligibility                                               model.EligibilityRepository
	bios                                                      corevalidationmodel.Repository
	before                                                    model.RoomControlSnapshot
	result                                                    model.Room
	readFailure, writeFailure, commitFailure, snapshotFailure error
	writes                                                    int
	seat                                                      model.RoomSeatPlan
	ready                                                     model.RoomReadyPlan
}

func (memory *roomControlMemory) WithWrite(_ context.Context, work func(model.RoomControlScope) error) error {
	if err := work(model.RoomControlScope{Read: memory, Write: memory, Eligibility: memory.eligibility, BIOS: memory.bios}); err != nil {
		return err
	}
	return memory.commitFailure
}

func (memory *roomControlMemory) Current(context.Context, string, string) (model.RoomControlSnapshot, error) {
	return memory.before, memory.readFailure
}

func (memory *roomControlMemory) Snapshot(context.Context, string) (model.Room, error) {
	return memory.result, memory.snapshotFailure
}

func (memory *roomControlMemory) Select(context.Context, model.RoomSelectionPlan) error {
	memory.writes++
	return memory.writeFailure
}

func (memory *roomControlMemory) Clear(context.Context, model.RoomClearPlan) error {
	memory.writes++
	return memory.writeFailure
}

func (memory *roomControlMemory) Seat(_ context.Context, plan model.RoomSeatPlan) error {
	memory.writes++
	memory.seat = plan
	return memory.writeFailure
}

func (memory *roomControlMemory) Ready(_ context.Context, plan model.RoomReadyPlan) error {
	memory.writes++
	memory.ready = plan
	return memory.writeFailure
}

func controlSnapshot() model.RoomControlSnapshot {
	return model.RoomControlSnapshot{RoomID: "room", HostID: "host", State: model.RoomStateWaiting, Version: 4, Selection: &model.RoomSelection{MaxPlayers: 2}, Member: &model.SeatMember{ID: "guest-member", ProfileID: "guest", Role: "GUEST", PlayerNo: 2, Version: 3}}
}

func TestRoomControlsValidateBeforeWriting(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, action string
		change       func(*model.RoomControlSnapshot)
		want         error
	}{
		{"draft seat", "seat", func(before *model.RoomControlSnapshot) { before.State = model.RoomStateDraft; before.Selection = nil }, model.ErrRoomConflict},
		{"draft ready", "ready", func(before *model.RoomControlSnapshot) { before.State = model.RoomStateDraft; before.Selection = nil }, model.ErrRoomConflict},
		{"stale seat", "seat", func(before *model.RoomControlSnapshot) { before.Version = 5 }, model.ErrPrecondition},
		{"outsider ready", "ready", func(before *model.RoomControlSnapshot) { before.Member = nil }, model.ErrForbidden},
		{"departed ready", "ready", func(before *model.RoomControlSnapshot) { at := int64(1); before.Member.LeftAtMS = &at }, model.ErrForbidden},
		{"host seat", "seat", func(before *model.RoomControlSnapshot) { before.Member.Role = "HOST" }, model.ErrForbidden},
		{"ready member seat", "seat", func(before *model.RoomControlSnapshot) { before.Member.Ready = true }, model.ErrRoomConflict},
		{"seat outside profile", "seat", func(before *model.RoomControlSnapshot) { before.Selection.MaxPlayers = 1 }, model.ErrInvalidSeat},
		{"occupied seat", "seat", func(before *model.RoomControlSnapshot) {
			before.Occupants = []model.SeatMember{{ProfileID: "other", PlayerNo: 2}}
		}, model.ErrSeatTaken},
		{"nonhost clear", "clear", func(*model.RoomControlSnapshot) {}, model.ErrForbidden},
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
		memory := &roomControlMemory{before: before, result: model.Room{RoomID: "room"}}
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
		memory.result = model.Room{RoomID: "room"}
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
	if err := validateSeat(before, 2, "host"); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("host seat mutation error=%v", err)
	}
}

func assertSeatPlan(t *testing.T, plan model.RoomSeatPlan, existing bool, now time.Time) {
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
