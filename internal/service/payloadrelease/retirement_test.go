package payloadrelease

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

type retirementMemory struct {
	bios                                                     BIOSRetirement
	launch                                                   LaunchRetirement
	releasedBIOS, releasedLaunch, removedBIOS, removedLaunch bool
	end                                                      LaunchRetirementEnd
	completed                                                RetirementCompletion
}

func (memory *retirementMemory) WithRetirement(_ context.Context, run func(RetirementScope) error) error {
	return run(RetirementScope{Read: memory, BIOS: memory, Launch: memory})
}

func (memory *retirementMemory) BIOS(context.Context, int) (BIOSRetirement, error) {
	return memory.bios, nil
}

func (memory *retirementMemory) Launch(context.Context, int64, int) (LaunchRetirement, error) {
	return memory.launch, nil
}
func (*retirementMemory) FenceBIOS(context.Context, BIOSRetirement) error { return nil }
func (memory *retirementMemory) ReleaseBIOSFiles(context.Context, BIOSRetirement) error {
	memory.removedBIOS = true
	return nil
}

func (memory *retirementMemory) CompleteBIOS(context.Context, BIOSRetirement, int64) error {
	memory.releasedBIOS = true
	return nil
}
func (*retirementMemory) FenceLaunch(context.Context, LaunchRetirement) error { return nil }
func (memory *retirementMemory) TerminateLaunch(_ context.Context, end LaunchRetirementEnd) error {
	memory.end = end
	return nil
}

func (memory *retirementMemory) ReleaseLaunchFiles(context.Context, LaunchRetirement) error {
	memory.removedLaunch = true
	return nil
}

func (memory *retirementMemory) CompleteLaunch(_ context.Context, change RetirementCompletion) error {
	memory.releasedLaunch = true
	memory.completed = change
	return nil
}

func TestRetirementsPreserveSharedBIOSAndWaitForBatchDrain(t *testing.T) {
	t.Parallel()
	for _, shared := range []bool{false, true} {
		memory := &retirementMemory{bios: BIOSRetirement{Found: true, ID: "old", BlobID: "bios", Version: 1, SharedActive: shared}}
		if !shared {
			memory.bios.Files = make([]RetirementFile, 200)
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
			memory := &retirementMemory{launch: LaunchRetirement{
				Found: true, ID: "launch", State: state, Version: 1,
				DueMS: 10, BootstrapMS: 10, Idle: WorkTime{Set: true, Value: 10}, HardMS: 20, Finished: WorkTime{Set: true, Value: 10},
			}}
			if state == "ACTIVE" {
				memory.launch.HardMS = 10
			}
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
		memory := &retirementMemory{launch: LaunchRetirement{
			Found: true, ID: "launch", State: "ACTIVE", Version: version,
			DueMS: 10, Idle: WorkTime{Set: true, Value: 11}, HardMS: 20,
		}}
		service := NewRetirements(memory, func() time.Time { return time.UnixMilli(10) })
		count, err := service.LaunchBatch(t.Context())
		if !errors.Is(err, ErrRetirementSnapshotChanged) || count != 0 || memory.removedLaunch || memory.releasedLaunch {
			t.Fatalf("live or overflowing launch retired: count=%d err=%v", count, err)
		}
	}
}
