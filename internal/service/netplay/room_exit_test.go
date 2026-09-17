package netplay

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	model "retrom/internal/model/netplay"
)

type roomExitMemory struct {
	before                 model.RoomExitSnapshot
	end                    *model.RoomEndPlan
	removal                *model.RoomRemovalPlan
	failure, commitFailure error
}

func (memory *roomExitMemory) WithExit(_ context.Context, work func(model.RoomExitScope) error) error {
	if err := work(model.RoomExitScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	return memory.commitFailure
}

func (memory *roomExitMemory) Current(context.Context, string, string) (model.RoomExitSnapshot, error) {
	return memory.before, memory.failure
}

func (memory *roomExitMemory) End(_ context.Context, plan model.RoomEndPlan) error {
	memory.end = &plan
	return memory.failure
}

func (memory *roomExitMemory) Remove(_ context.Context, plan model.RoomRemovalPlan) error {
	memory.removal = &plan
	return memory.failure
}

func roomExitFixture() (*RoomExit, *roomExitMemory) {
	session := "session"
	guest := model.SeatMember{ID: "guest-member", ProfileID: "guest", Role: "GUEST", PlayerNo: 2, Version: 3}
	before := model.RoomExitSnapshot{Room: model.RoomControlSnapshot{RoomID: "room", HostID: "host", State: model.RoomStateRunning, Version: 7, Selection: &model.RoomSelection{GameID: "game"}, Member: &guest, Occupants: []model.SeatMember{guest}}, SessionID: &session}
	memory := &roomExitMemory{before: before}
	return NewRoomExit(memory, time.Hour, func() time.Time { return time.UnixMilli(1786000000000) }), memory
}

func TestRoomExitAuthorizationAndVersions(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"guest close", "outsider", "stale", "draft", "host leave", "guest kick"} {
		t.Run(name, func(t *testing.T) {
			service, memory := roomExitFixture()
			version := int64(7)
			actor, reason := "guest", "USER_EXIT"
			want := model.ErrForbidden
			switch name {
			case "guest close":
				reason = "HOST_CLOSED"
			case "outsider":
				memory.before.Room.Member = nil
			case "stale":
				version--
				want = model.ErrPrecondition
			case "draft":
				memory.before.Room.State = model.RoomStateDraft
				memory.before.Room.Selection = nil
				want = model.ErrRoomConflict
			}
			var err error
			switch name {
			case "host leave":
				memory.before.Room.Member.Role = "HOST"
				err = service.Leave(t.Context(), "room", "host", version)
			case "guest kick":
				err = service.Kick(t.Context(), "room", "guest", "guest-member", version)
			default:
				err = service.End(t.Context(), "room", actor, reason, &version)
			}
			if !errors.Is(err, want) || memory.end != nil || memory.removal != nil {
				t.Fatalf("error=%v end=%+v removal=%+v", err, memory.end, memory.removal)
			}
		})
	}
}

func TestRoomExitPlansPreserveSessionDisposition(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, actor, reason, disposition, sessionState, playState, leaveProfile, leaveReason, endReason string }{
		{"guest exit", "guest", "USER_EXIT", "WAITING", "FINISHED", "FINISHED", "guest", "USER_LEFT", "USER_EXIT"},
		{"host exit", "host", "USER_EXIT", "WAITING", "FINISHED", "FINISHED", "", "", "USER_EXIT"},
		{"host close", "host", "HOST_CLOSED", "ENDED", "FAILED", "ABANDONED", "", "", "HOST_CLOSED"},
		{"host timeout", "host", "PEER_TIMEOUT", "ENDED", "FAILED", "ABANDONED", "", "", "HOST_LOST"},
		{"system restart", "", "SERVER_RESTARTED", "ENDED", "FAILED", "ABANDONED", "", "", "SERVER_RESTARTED"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			service, memory := roomExitFixture()
			var err error
			if item.actor == "" {
				err = service.EndSystem(t.Context(), "room", item.reason)
			} else {
				err = service.End(t.Context(), "room", item.actor, item.reason, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			plan := memory.end
			if plan == nil || plan.Before.Room.Version != 7 || plan.Before.SessionID == nil {
				t.Fatalf("end plan=%+v", plan)
			}
			got := []string{plan.Disposition, plan.SessionState, plan.PlayState, plan.LeaveProfileID, plan.LeaveReason, plan.Reason}
			want := []string{item.disposition, item.sessionState, item.playState, item.leaveProfile, item.leaveReason, item.endReason}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("plan=%+v want=%+v", got, want)
			}
			if item.actor == "" && plan.ActorID != nil {
				t.Fatal("system end has actor")
			}
		})
	}
}

func TestRoomExitTerminalIsIdempotent(t *testing.T) {
	t.Parallel()
	service, memory := roomExitFixture()
	memory.before.Room.State = "ENDED"
	if err := service.End(t.Context(), "room", "guest", "USER_EXIT", nil); err != nil {
		t.Fatal(err)
	}
	if memory.end != nil {
		t.Fatal("terminal room written")
	}
}

func TestRoomExitLeaveUsesOneVersionedTransaction(t *testing.T) {
	t.Parallel()
	service, memory := roomExitFixture()
	if err := service.Leave(t.Context(), "room", "guest", 7); err != nil {
		t.Fatal(err)
	}
	if memory.end == nil || memory.end.Before.Room.Version != 7 || memory.end.LeaveProfileID != "guest" {
		t.Fatalf("leave=%+v", memory.end)
	}
	sentinel := errors.New("commit failed")
	memory.commitFailure = sentinel
	if err := service.Leave(t.Context(), "room", "guest", 7); !errors.Is(err, sentinel) {
		t.Fatalf("commit=%v", err)
	}
}

func TestRoomExitWaitingRemovesOnlyGuest(t *testing.T) {
	t.Parallel()
	for _, kick := range []bool{false, true} {
		t.Run(map[bool]string{false: "leave", true: "kick"}[kick], func(t *testing.T) {
			service, memory := roomExitFixture()
			memory.before.Room.State = model.RoomStateWaiting
			memory.before.SessionID = nil
			var err error
			if kick {
				err = service.Kick(t.Context(), "room", "host", "guest-member", 7)
			} else {
				err = service.Leave(t.Context(), "room", "guest", 7)
			}
			if err != nil {
				t.Fatal(err)
			}
			plan := memory.removal
			if plan == nil || plan.Member.ID != "guest-member" || plan.Member.Version != 3 || plan.Evidence.ExpiresAtMS != 1786003600000 || memory.end != nil {
				t.Fatalf("remove=%+v", plan)
			}
			want := "USER_LEFT"
			if kick {
				want = "HOST_KICKED"
			}
			if plan.Reason != want {
				t.Fatalf("reason=%s", plan.Reason)
			}
		})
	}
}
