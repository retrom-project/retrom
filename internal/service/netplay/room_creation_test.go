package netplay

import (
	"context"
	"errors"
	"testing"
	"time"
)

type roomCreationMemory struct {
	capacity                             RoomCapacity
	plan                                 RoomCreationPlan
	failure, commitFailure, writeFailure error
	inserts                              int
}

func (memory *roomCreationMemory) WithCreate(_ context.Context, work func(RoomCreationWriter) error) error {
	if err := work(memory); err != nil {
		return err
	}
	return memory.commitFailure
}

func (memory *roomCreationMemory) Capacity(context.Context, string) (RoomCapacity, error) {
	return memory.capacity, memory.failure
}

func (memory *roomCreationMemory) Insert(_ context.Context, plan RoomCreationPlan) (Room, error) {
	memory.inserts++
	memory.plan = plan
	return Room{RoomID: plan.RoomID, State: RoomStateDraft, Version: 1, ExpiresAtMS: plan.ExpiresAtMS, Members: []RoomMember{{MemberID: plan.MemberID, ProfileID: plan.HostID, Role: "HOST", PlayerNo: 1}}}, memory.writeFailure
}

func TestRoomCreationRejectsCapacityAndDuplicateHostWithoutWrites(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		capacity RoomCapacity
		want     error
	}{{RoomCapacity{Active: 16}, ErrCapacity}, {RoomCapacity{Active: 1, HostActive: true}, ErrRoomConflict}} {
		memory := &roomCreationMemory{capacity: test.capacity}
		_, err := NewRoomCreation(memory, 16, time.Hour, time.Now).Create(t.Context(), "host")
		if !errors.Is(err, test.want) || memory.inserts != 0 {
			t.Fatalf("error=%v writes=%d", err, memory.inserts)
		}
	}
}

func TestRoomCreationReturnsOnlyCommittedHostProjection(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_786_000_000_000)
	memory := &roomCreationMemory{}
	service := NewRoomCreation(memory, 16, time.Minute, func() time.Time { return now })
	room, err := service.Create(t.Context(), "host")
	if err != nil || room.RoomID == "" || room.Version != 1 || !room.Permissions.Host || room.SelfMemberID == nil || room.ExpiresAtMS != now.Add(time.Minute).UnixMilli() {
		t.Fatalf("created room=%+v error=%v", room, err)
	}
	if memory.plan.RoomID == memory.plan.MemberID || memory.plan.HostID != "host" || string(memory.plan.Event) != `{"schemaVersion":1}` {
		t.Fatalf("plan=%+v", memory.plan)
	}
	sentinel := errors.New("commit failed")
	memory.commitFailure = sentinel
	room, err = service.Create(t.Context(), "host")
	if !errors.Is(err, sentinel) || room.RoomID != "" || room.SelfMemberID != nil {
		t.Fatalf("failed commit leaked room=%+v error=%v", room, err)
	}
}

func TestRoomIdentityFailureDoesNotPersistPartialRecords(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("entropy unavailable")
	for _, failAt := range []int{1, 2} {
		memory := &roomCreationMemory{}
		service := NewRoomCreation(memory, 16, time.Hour, time.Now)
		calls := 0
		service.newID = func() (string, error) {
			calls++
			if calls == failAt {
				return "", sentinel
			}
			return "generated", nil
		}
		room, err := service.Create(t.Context(), "host")
		if !errors.Is(err, sentinel) || room.RoomID != "" || memory.inserts != 0 {
			t.Fatalf("failed identity room=%+v writes=%d error=%v", room, memory.inserts, err)
		}
	}
}

func TestRoomCreationReturnsNoProjectionOnStorageFailure(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("storage failed")
	for _, memory := range []*roomCreationMemory{{failure: sentinel}, {writeFailure: sentinel}} {
		room, err := NewRoomCreation(memory, 16, time.Hour, time.Now).Create(t.Context(), "host")
		if !errors.Is(err, sentinel) || room.RoomID != "" || room.SelfMemberID != nil {
			t.Fatalf("storage failure leaked room=%+v error=%v", room, err)
		}
	}
}
