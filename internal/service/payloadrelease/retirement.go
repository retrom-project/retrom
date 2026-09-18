package payloadrelease

import (
	"context"
	"fmt"
	"math"
	"time"

	model "retrom/internal/model/payloadrelease"
)

const retirementBatchSize = 200

type Retirements struct {
	repository model.RetirementRepository
	now        func() time.Time
}

func NewRetirements(repository model.RetirementRepository, now func() time.Time) *Retirements {
	if now == nil {
		now = time.Now
	}
	return &Retirements{repository: repository, now: now}
}

func (service *Retirements) BIOS(ctx context.Context) error {
	for {
		worked, err := service.BIOSBatch(ctx)
		if err != nil || !worked {
			return err
		}
	}
}

func (service *Retirements) BIOSBatch(ctx context.Context) (bool, error) {
	before, err := service.repository.LoadBIOSRetirement(ctx, retirementBatchSize)
	if err != nil {
		return false, fmt.Errorf("commit BIOS retirement: %w", fmt.Errorf("read retiring BIOS: %w", err))
	}
	if !before.Found {
		return false, nil
	}
	if before.ID == "" || before.BlobID == "" || before.Version < 1 || before.Version == math.MaxInt64 ||
		before.SharedActive && len(before.Files) != 0 {
		return false, fmt.Errorf("commit BIOS retirement: %w", model.ErrRetirementSnapshotChanged)
	}
	plan := model.BIOSRetirementPlan{
		Before:       before,
		ReleaseFiles: !before.SharedActive,
		Complete:     len(before.Files) < retirementBatchSize,
		NowMS:        service.now().UnixMilli(),
	}
	if err := service.repository.CommitBIOSRetirement(ctx, plan); err != nil {
		return false, fmt.Errorf("commit BIOS retirement: %w", err)
	}
	return true, nil
}

func (service *Retirements) Launches(ctx context.Context) error {
	for {
		count, err := service.LaunchBatch(ctx)
		if err != nil || count == 0 {
			return err
		}
	}
}

func (service *Retirements) LaunchBatch(ctx context.Context) (int, error) {
	now := service.now().UnixMilli()
	before, err := service.repository.LoadLaunchRetirement(ctx, now, retirementBatchSize)
	if err != nil {
		return 0, fmt.Errorf("commit launch retirement: %w", fmt.Errorf("read retiring launch: %w", err))
	}
	if !before.Found {
		return 0, nil
	}
	if !validRetirement(before, now) {
		return 0, fmt.Errorf("commit launch retirement: %w", model.ErrRetirementSnapshotChanged)
	}
	end := model.LaunchRetirementEnd{Before: before, State: before.State, PlayState: "ABANDONED", NowMS: now}
	end.Expire = before.State == "CREATED" || before.State == "ACTIVE"
	if end.Expire {
		end.State = "EXPIRED"
	}
	plan := model.LaunchRetirementPlan{Before: before, End: end}
	if len(before.Content) < retirementBatchSize && len(before.External) < retirementBatchSize {
		due := before.DueMS
		if end.Expire {
			due = now
		}
		completion := model.RetirementCompletion{ID: before.ID, DueMS: due, NowMS: now}
		plan.Complete = &completion
	}
	if err := service.repository.CommitLaunchRetirement(ctx, plan); err != nil {
		return 0, fmt.Errorf("commit launch retirement: %w", err)
	}
	return 1, nil
}

func validRetirement(before model.LaunchRetirement, now int64) bool {
	if before.ID == "" || before.Version < 1 || before.Version == math.MaxInt64 || before.DueMS > now {
		return false
	}
	for _, play := range before.Plays {
		if play.ID == "" || play.Version < 1 || play.Version == math.MaxInt64 {
			return false
		}
	}
	switch before.State {
	case "CREATED":
		return min(before.BootstrapMS, before.HardMS) == before.DueMS
	case "ACTIVE":
		return before.Idle.Set && min(before.Idle.Value, before.HardMS) == before.DueMS
	case "FINISHED", "EXPIRED", "REVOKED":
		return before.Finished.Set && before.Finished.Value == before.DueMS
	default:
		return false
	}
}
