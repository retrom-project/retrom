package cleanupjobs_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	jobs "retrom/internal/service/cleanupjobs"
	bios "retrom/internal/service/firmware/retirement"
	launch "retrom/internal/service/launch/retirement"
)

type retirementMemory struct {
	bios                                                     jobs.BIOSRetirement
	launch                                                   jobs.LaunchRetirement
	releasedBIOS, releasedLaunch, removedBIOS, removedLaunch bool
	end                                                      jobs.LaunchRetirementEnd
	completed                                                jobs.RetirementCompletion
}

func (memory *retirementMemory) WithBIOSRetirement(_ context.Context, run func(jobs.BIOSRetirementScope) error) error {
	return run(jobs.BIOSRetirementScope{Read: memory, BIOS: memory})
}

func (memory *retirementMemory) WithLaunchRetirement(_ context.Context, run func(jobs.LaunchRetirementScope) error) error {
	return run(jobs.LaunchRetirementScope{Read: memory, Launch: memory})
}

func (memory *retirementMemory) BIOS(context.Context, int) (jobs.BIOSRetirement, error) {
	return memory.bios, nil
}

func (memory *retirementMemory) Launch(context.Context, int64, int) (jobs.LaunchRetirement, error) {
	return memory.launch, nil
}
func (*retirementMemory) FenceBIOS(context.Context, jobs.BIOSRetirement) error { return nil }
func (memory *retirementMemory) ReleaseBIOSFiles(context.Context, jobs.BIOSRetirement) error {
	memory.removedBIOS = true
	return nil
}

func (memory *retirementMemory) CompleteBIOS(context.Context, jobs.BIOSRetirement, int64) error {
	memory.releasedBIOS = true
	return nil
}
func (*retirementMemory) FenceLaunch(context.Context, jobs.LaunchRetirement) error { return nil }
func (memory *retirementMemory) TerminateLaunch(_ context.Context, end jobs.LaunchRetirementEnd) error {
	memory.end = end
	return nil
}

func (memory *retirementMemory) ReleaseLaunchFiles(context.Context, jobs.LaunchRetirement) error {
	memory.removedLaunch = true
	return nil
}

func (memory *retirementMemory) CompleteLaunch(_ context.Context, change jobs.RetirementCompletion) error {
	memory.releasedLaunch = true
	memory.completed = change
	return nil
}

func TestRetirementsWaitForBIOSReaderBatchDrain(t *testing.T) {
	for _, count := range []int{0, 199, 200} {
		memory := &retirementMemory{bios: jobs.BIOSRetirement{Found: true, ID: "old", BlobID: "bios", Version: 1, Files: make([]jobs.RetirementFile, count)}}
		service := bios.New(memory, func() time.Time { return time.UnixMilli(10) })
		worked, err := service.BIOSBatch(t.Context())
		if err != nil || !worked || !memory.removedBIOS || memory.releasedBIOS != (count < 200) {
			t.Fatalf("count=%d worked=%v complete=%v err=%v", count, worked, memory.releasedBIOS, err)
		}
	}
}

func TestRetirementUsesActualLaunchDeadlineAndPreservesTerminalState(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"CREATED", "ACTIVE", "FINISHED", "REVOKED"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			memory := &retirementMemory{launch: jobs.LaunchRetirement{
				Found: true, ID: "launch", State: state, Version: 1,
				DueMS: 10, BootstrapMS: 10, Idle: jobs.WorkTime{Set: true, Value: 10}, HardMS: 20, Finished: jobs.WorkTime{Set: true, Value: 10},
			}}
			if state == "ACTIVE" {
				memory.launch.HardMS = 10
			}
			service := launch.New(memory, func() time.Time { return time.UnixMilli(10) })
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
		memory := &retirementMemory{launch: jobs.LaunchRetirement{
			Found: true, ID: "launch", State: "ACTIVE", Version: version,
			DueMS: 10, Idle: jobs.WorkTime{Set: true, Value: 11}, HardMS: 20,
		}}
		service := launch.New(memory, func() time.Time { return time.UnixMilli(10) })
		count, err := service.LaunchBatch(t.Context())
		if !errors.Is(err, jobs.ErrRetirementSnapshotChanged) || count != 0 || memory.removedLaunch || memory.releasedLaunch {
			t.Fatalf("live or overflowing launch retired: count=%d err=%v", count, err)
		}
	}
}
