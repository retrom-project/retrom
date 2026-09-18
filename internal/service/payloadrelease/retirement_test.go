package payloadrelease

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	model "retrom/internal/model/payloadrelease"
)

type retirementMemory struct {
	bios                                                     model.BIOSRetirement
	launch                                                   model.LaunchRetirement
	releasedBIOS, releasedLaunch, removedBIOS, removedLaunch bool
	end                                                      model.LaunchRetirementEnd
	completed                                                model.RetirementCompletion
}

func (memory *retirementMemory) LoadBIOSRetirement(_ context.Context, _ int) (model.BIOSRetirement, error) {
	return memory.bios, nil
}

func (memory *retirementMemory) LoadLaunchRetirement(_ context.Context, _ int64, _ int) (model.LaunchRetirement, error) {
	return memory.launch, nil
}

func (memory *retirementMemory) CommitBIOSRetirement(_ context.Context, plan model.BIOSRetirementPlan) error {
	if plan.ReleaseFiles {
		memory.removedBIOS = true
	}
	if plan.Complete {
		memory.releasedBIOS = true
	}
	return nil
}

func (memory *retirementMemory) CommitLaunchRetirement(_ context.Context, plan model.LaunchRetirementPlan) error {
	memory.end = plan.End
	memory.removedLaunch = true
	memory.releasedLaunch = plan.Complete != nil
	if plan.Complete != nil {
		memory.completed = *plan.Complete
	}
	return nil
}

func TestRetirementsPreserveSharedBIOSAndWaitForBatchDrain(t *testing.T) {
	t.Parallel()
	for _, shared := range []bool{false, true} {
		memory := &retirementMemory{bios: model.BIOSRetirement{Found: true, ID: "old", BlobID: "bios", Version: 1, SharedActive: shared}}
		if !shared {
			memory.bios.Files = make([]model.RetirementFile, 200)
		}
		service := NewRetirements(memory, func() time.Time { return time.UnixMilli(10) })
		worked, err := service.BIOSBatch(t.Context())
		if err != nil || !worked || memory.removedBIOS == shared || memory.releasedBIOS != shared {
			t.Fatalf("wrong BIOS retirement policy shared=%v removed=%v complete=%v err=%v",
				shared, memory.removedBIOS, memory.releasedBIOS, err)
		}
	}
}

func TestRetirementUsesActualLaunchDeadlineAndPreservesTerminalState(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"CREATED", "ACTIVE", "FINISHED", "REVOKED"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			memory := &retirementMemory{launch: model.LaunchRetirement{
				Found: true, ID: "launch", State: state, Version: 1,
				DueMS: 10, BootstrapMS: 10, Idle: model.WorkTime{Set: true, Value: 10}, HardMS: 20, Finished: model.WorkTime{Set: true, Value: 10},
			}}
			service := NewRetirements(memory, func() time.Time { return time.UnixMilli(10) })
			count, err := service.LaunchBatch(t.Context())
			if err != nil || count != 1 || !memory.removedLaunch || !memory.releasedLaunch {
				t.Fatalf("launch retirement state=%s count=%d err=%v", state, count, err)
			}
			active := state == "CREATED" || state == "ACTIVE"
			if memory.end.Expire != active || active && memory.end.State != "EXPIRED" || memory.completed.DueMS != 10 {
				t.Fatalf("changed wrong launch lifecycle: end=%+v complete=%+v", memory.end, memory.completed)
			}
		})
	}
}

func TestRetirementRejectsLiveLaunchAndVersionOverflow(t *testing.T) {
	t.Parallel()
	for _, version := range []int64{1, math.MaxInt64} {
		memory := &retirementMemory{launch: model.LaunchRetirement{
			Found: true, ID: "launch", State: "ACTIVE", Version: version,
			DueMS: 10, Idle: model.WorkTime{Set: true, Value: 11}, HardMS: 20,
		}}
		service := NewRetirements(memory, func() time.Time { return time.UnixMilli(10) })
		count, err := service.LaunchBatch(t.Context())
		if !errors.Is(err, model.ErrRetirementSnapshotChanged) || count != 0 || memory.removedLaunch || memory.releasedLaunch {
			t.Fatalf("live or overflowing launch retired: count=%d err=%v", count, err)
		}
	}
}
