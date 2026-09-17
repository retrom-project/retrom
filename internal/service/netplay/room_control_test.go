package netplay

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/netplay"
)

type roomControlMemory struct {
	before                                                    model.RoomControlSnapshot
	result                                                    model.Room
	readFailure, writeFailure, commitFailure, snapshotFailure error
	writes                                                    int
	seat                                                      model.RoomSeatPlan
	ready                                                     model.RoomReadyPlan
}

func (memory *roomControlMemory) LoadControlSnapshot(
	_ context.Context, _, _ string,
) (model.RoomControlSnapshot, error) {
	return memory.before, memory.readFailure
}

// Current satisfies RoomControlReader for session start tests.
func (memory *roomControlMemory) Current(
	_ context.Context, _, _ string,
) (model.RoomControlSnapshot, error) {
	return memory.before, memory.readFailure
}

// Snapshot satisfies RoomControlReader for session start tests.
func (memory *roomControlMemory) Snapshot(
	_ context.Context, _ string,
) (model.Room, error) {
	return memory.result, memory.snapshotFailure
}

func (memory *roomControlMemory) commitRoom(
	cmd struct {
		actorID string
		version int64
		host    bool
		states  []string
	},
	apply func(model.RoomControlSnapshot) error,
) (model.Room, error) {
	before := memory.before
	if memory.readFailure != nil {
		return model.Room{}, memory.readFailure
	}
	if cmd.host && before.HostID != cmd.actorID {
		return model.Room{}, model.ErrForbidden
	}
	if before.Version != cmd.version {
		return model.Room{}, model.ErrPrecondition
	}
	found := false
	for _, s := range cmd.states {
		if s == before.State {
			found = true
			break
		}
	}
	if !found {
		return model.Room{}, model.ErrRoomConflict
	}
	if err := apply(before); err != nil {
		return model.Room{}, err
	}
	if memory.snapshotFailure != nil {
		return model.Room{}, memory.snapshotFailure
	}
	if memory.commitFailure != nil {
		return model.Room{}, memory.commitFailure
	}
	return memory.result, nil
}

func (memory *roomControlMemory) CommitSelectGame(
	_ context.Context, cmd model.SelectGameCommand,
) (model.Room, error) {
	return memory.commitRoom(
		struct {
			actorID string
			version int64
			host    bool
			states  []string
		}{
			cmd.ActorID, cmd.Version, true,
			[]string{model.RoomStateDraft, model.RoomStateWaiting},
		},
		func(before model.RoomControlSnapshot) error {
			for _, member := range before.Occupants {
				if member.PlayerNo > cmd.Selection.MaxPlayers {
					return model.ErrInvalidSeat
				}
			}
			memory.writes++
			return memory.writeFailure
		},
	)
}

func (memory *roomControlMemory) CommitClearGame(
	_ context.Context, cmd model.ClearGameCommand,
) (model.Room, error) {
	return memory.commitRoom(
		struct {
			actorID string
			version int64
			host    bool
			states  []string
		}{cmd.ActorID, cmd.Version, true, []string{model.RoomStateWaiting}},
		func(_ model.RoomControlSnapshot) error {
			memory.writes++
			return memory.writeFailure
		},
	)
}

func (memory *roomControlMemory) CommitSetSeat(
	_ context.Context, cmd model.SetSeatCommand,
) (model.Room, error) {
	return memory.commitRoom(
		struct {
			actorID string
			version int64
			host    bool
			states  []string
		}{cmd.ActorID, cmd.Version, false, []string{model.RoomStateWaiting}},
		func(before model.RoomControlSnapshot) error {
			if err := model.ValidateSeat(before, cmd.PlayerNo, cmd.ActorID); err != nil {
				return err
			}
			plan, err := model.BuildSeatPlan(
				before, cmd.ActorID, cmd.PlayerNo,
				cmd.NewMemberID, cmd.NowMS, cmd.IdleMS,
			)
			if err != nil {
				return err
			}
			memory.seat = plan
			memory.writes++
			return memory.writeFailure
		},
	)
}

func (memory *roomControlMemory) CommitSetReady(
	_ context.Context, cmd model.SetReadyCommand,
) (model.Room, error) {
	return memory.commitRoom(
		struct {
			actorID string
			version int64
			host    bool
			states  []string
		}{cmd.ActorID, cmd.Version, false, []string{model.RoomStateWaiting}},
		func(before model.RoomControlSnapshot) error {
			if before.Member == nil || before.Member.LeftAtMS != nil {
				return model.ErrForbidden
			}
			memory.ready = model.RoomReadyPlan{Before: before, Ready: cmd.Ready}
			memory.writes++
			return memory.writeFailure
		},
	)
}

func controlSnapshot() model.RoomControlSnapshot {
	return model.RoomControlSnapshot{
		RoomID: "room", HostID: "host",
		State: model.RoomStateWaiting, Version: 4,
		Selection: &model.RoomSelection{MaxPlayers: 2},
		Member: &model.SeatMember{
			ID: "guest-member", ProfileID: "guest",
			Role: "GUEST", PlayerNo: 2, Version: 3,
		},
	}
}

func TestRoomControlsValidateBeforeWriting(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, action string
		change       func(*model.RoomControlSnapshot)
		want         error
	}{
		{"draft seat", "seat", func(before *model.RoomControlSnapshot) {
			before.State = model.RoomStateDraft
			before.Selection = nil
		}, model.ErrRoomConflict},
		{"draft ready", "ready", func(before *model.RoomControlSnapshot) {
			before.State = model.RoomStateDraft
			before.Selection = nil
		}, model.ErrRoomConflict},
		{"stale seat", "seat", func(before *model.RoomControlSnapshot) {
			before.Version = 5
		}, model.ErrPrecondition},
		{"outsider ready", "ready", func(before *model.RoomControlSnapshot) {
			before.Member = nil
		}, model.ErrForbidden},
		{"departed ready", "ready", func(before *model.RoomControlSnapshot) {
			at := int64(1)
			before.Member.LeftAtMS = &at
		}, model.ErrForbidden},
		{"host seat", "seat", func(before *model.RoomControlSnapshot) {
			before.Member.Role = "HOST"
		}, model.ErrForbidden},
		{"ready member seat", "seat", func(before *model.RoomControlSnapshot) {
			before.Member.Ready = true
		}, model.ErrRoomConflict},
		{"seat outside profile", "seat", func(before *model.RoomControlSnapshot) {
			before.Selection.MaxPlayers = 1
		}, model.ErrInvalidSeat},
		{"occupied seat", "seat", func(before *model.RoomControlSnapshot) {
			before.Occupants = []model.SeatMember{{ProfileID: "other", PlayerNo: 2}}
		}, model.ErrSeatTaken},
		{"nonhost clear", "clear", func(*model.RoomControlSnapshot) {}, model.ErrForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := controlSnapshot()
			test.change(&before)
			memory := &roomControlMemory{before: before}
			service := NewRoomControl(memory, nil, nil, nil,
				time.Hour, time.Hour, time.Now)
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
		service := NewRoomControl(memory, nil, nil, nil,
			time.Minute, time.Hour, func() time.Time { return now })
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
	for _, memory := range []*roomControlMemory{
		{readFailure: sentinel},
		{writeFailure: sentinel},
		{snapshotFailure: sentinel},
		{commitFailure: sentinel},
	} {
		memory.before = controlSnapshot()
		memory.result = model.Room{RoomID: "room"}
		service := NewRoomControl(memory, nil, nil, nil,
			time.Hour, time.Hour, time.Now)
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
	service := NewRoomControl(memory, nil, nil, nil,
		time.Hour, time.Hour, time.Now)
	service.newID = func() (string, error) { return "", sentinel }
	_, err := service.SetSeat(t.Context(), "room", "guest", 2, 4)
	if !errors.Is(err, sentinel) || memory.writes != 0 {
		t.Fatalf("identity error=%v writes=%d", err, memory.writes)
	}
}

func TestHostCannotClaimGuestSeatThroughTheModel(t *testing.T) {
	t.Parallel()
	before := controlSnapshot()
	before.Member.Role = "HOST"
	if err := model.ValidateSeat(before, 2, "host"); !errors.Is(err, model.ErrForbidden) {
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
	if event.SchemaVersion != 1 || event.ToPlayerNo != 2 ||
		plan.Evidence.ExpiresAtMS != now.Add(time.Hour).UnixMilli() {
		t.Fatalf("seat plan=%+v event=%+v", plan, event)
	}
	if existing {
		if plan.MemberID != "guest-member" || plan.Evidence.Type != "SEAT_CHANGED" ||
			event.FromPlayerNo == nil || *event.FromPlayerNo != 2 {
			t.Fatalf("move plan=%+v event=%+v", plan, event)
		}
	} else if plan.MemberID != "new-member" || plan.Evidence.Type != "MEMBER_JOINED" ||
		event.FromPlayerNo != nil {
		t.Fatalf("join plan=%+v event=%+v", plan, event)
	}
}
