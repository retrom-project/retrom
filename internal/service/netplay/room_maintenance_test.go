package netplay

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/netplay"
)

type roomMaintenanceMemory struct {
	passive, active                          []model.ExpiryCandidate
	cutoffs                                  model.ExpiryCutoffs
	expired                                  []model.ExpiryPlan
	recovered                                []model.RecoveryPlan
	ended                                    []model.ExpiryCandidate
	readFailure, writeFailure, commitFailure error
}

func (memory *roomMaintenanceMemory) Passive(_ context.Context, cutoffs model.ExpiryCutoffs) ([]model.ExpiryCandidate, error) {
	memory.cutoffs = cutoffs
	return memory.passive, memory.readFailure
}

func (memory *roomMaintenanceMemory) Active(_ context.Context, cutoffs model.ExpiryCutoffs) ([]model.ExpiryCandidate, error) {
	memory.cutoffs = cutoffs
	return memory.active, memory.readFailure
}

func (memory *roomMaintenanceMemory) CommitExpiry(_ context.Context, plan model.ExpiryPlan) error {
	if memory.writeFailure != nil {
		return memory.writeFailure
	}
	memory.expired = append(memory.expired, plan)
	return memory.commitFailure
}

func (memory *roomMaintenanceMemory) CommitRecovery(_ context.Context, plan model.RecoveryPlan) error {
	if memory.writeFailure != nil {
		return memory.writeFailure
	}
	memory.recovered = append(memory.recovered, plan)
	return memory.commitFailure
}

func (memory *roomMaintenanceMemory) EndExpired(_ context.Context, candidate model.ExpiryCandidate, _ int64) error {
	memory.ended = append(memory.ended, candidate)
	return memory.writeFailure
}

func TestRoomMaintenanceUsesBoundedDeadlineCandidates(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1786000000000)
	memory := &roomMaintenanceMemory{passive: []model.ExpiryCandidate{{RoomID: "draft", HostID: "host", State: model.RoomStateDraft, Version: 4}}, active: []model.ExpiryCandidate{{RoomID: "starting", State: model.RoomStateStarting, Version: 5}, {RoomID: "running", State: model.RoomStateRunning, Version: 6}}}
	service := NewRoomMaintenance(memory, memory, func() time.Time { return now })
	if err := service.Expire(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := model.ExpiryCutoffs{Now: now.UnixMilli(), StartingBefore: now.Add(-2 * time.Minute).UnixMilli(), RunningBefore: now.Add(-8 * time.Hour).UnixMilli(), Limit: 100}
	if memory.cutoffs != want || len(memory.expired) != 1 || len(memory.ended) != 2 {
		t.Fatalf("cutoffs=%+v expired=%v ended=%v", memory.cutoffs, memory.expired, memory.ended)
	}
	if memory.expired[0].Before.Version != 4 || memory.expired[0].Now != now.UnixMilli() {
		t.Fatalf("expiry=%+v", memory.expired[0])
	}
}

func TestRoomMaintenanceRecoveryChecksReasonAndCommit(t *testing.T) {
	t.Parallel()
	memory := &roomMaintenanceMemory{}
	service := NewRoomMaintenance(memory, memory, func() time.Time { return time.UnixMilli(1000) })
	if err := service.Recover(t.Context(), "USER_EXIT"); !errors.Is(err, model.ErrInvalidRecoveryReason) || len(memory.recovered) != 0 {
		t.Fatalf("invalid recovery=%v", err)
	}
	for _, reason := range []string{"SERVER_RESTARTED", "RESTORE"} {
		if err := service.Recover(t.Context(), reason); err != nil {
			t.Fatal(err)
		}
	}
	if len(memory.recovered) != 2 || memory.recovered[1].Reason != "RESTORE" || memory.recovered[1].Now != 1000 {
		t.Fatalf("recovery=%v", memory.recovered)
	}
	sentinel := errors.New("commit failed")
	memory.commitFailure = sentinel
	if err := service.Recover(t.Context(), "RESTORE"); !errors.Is(err, sentinel) {
		t.Fatalf("commit error=%v", err)
	}
}

func TestRoomMaintenancePropagatesFailures(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("storage failure")
	for _, phase := range []string{"read", "write", "commit", "active"} {
		t.Run(phase, func(t *testing.T) {
			memory := &roomMaintenanceMemory{passive: []model.ExpiryCandidate{{RoomID: "draft"}}}
			switch phase {
			case "read":
				memory.readFailure = sentinel
			case "write":
				memory.writeFailure = sentinel
			case "commit":
				memory.commitFailure = sentinel
			case "active":
				memory.passive = nil
				memory.active = []model.ExpiryCandidate{{RoomID: "running"}}
				memory.writeFailure = sentinel
			}
			if err := NewRoomMaintenance(memory, memory, time.Now).Expire(t.Context()); !errors.Is(err, sentinel) {
				t.Fatalf("expiry failure=%v", err)
			}
		})
	}
}

func TestRoomExpiryDoesNotEndReplacedSession(t *testing.T) {
	t.Parallel()
	service, memory := roomExitFixture()
	candidate := model.ExpiryCandidate{RoomID: "room", State: model.RoomStateRunning, Version: 7, SessionID: memory.before.SessionID}
	for _, change := range []string{"version", "session", "state"} {
		t.Run(change, func(t *testing.T) {
			service, memory = roomExitFixture()
			switch change {
			case "version":
				memory.before.Room.Version++
			case "session":
				id := "replacement"
				memory.before.SessionID = &id
			case "state":
				memory.before.Room.State = model.RoomStateWaiting
			}
			if err := service.EndExpired(t.Context(), candidate, 1786000000000); err != nil {
				t.Fatal(err)
			}
			if memory.end != nil {
				t.Fatalf("stale expiry wrote=%+v", memory.end)
			}
		})
	}
}
