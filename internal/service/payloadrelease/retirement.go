package payloadrelease

import (
	"context"
	"fmt"
	"math"
	"time"
)

const retirementBatchSize = 200

type Retirements struct {
	repository RetirementRepository
	now        func() time.Time
}

func NewRetirements(repository RetirementRepository, now func() time.Time) *Retirements {
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
	worked := false
	err := service.repository.WithRetirement(ctx, func(scope RetirementScope) error {
		before, err := scope.Read.BIOS(ctx, retirementBatchSize)
		if err != nil {
			return fmt.Errorf("read retiring BIOS: %w", err)
		}
		if !before.Found {
			return nil
		}
		if before.ID == "" || before.BlobID == "" || before.Version < 1 || before.Version == math.MaxInt64 ||
			before.SharedActive && len(before.Files) != 0 {
			return ErrRetirementSnapshotChanged
		}
		if err := scope.BIOS.FenceBIOS(ctx, before); err != nil {
			return fmt.Errorf("fence retiring BIOS: %w", err)
		}
		if !before.SharedActive {
			if err := scope.BIOS.ReleaseBIOSFiles(ctx, before); err != nil {
				return fmt.Errorf("release stale BIOS files: %w", err)
			}
		}
		if len(before.Files) < retirementBatchSize {
			if err := scope.BIOS.CompleteBIOS(ctx, before, service.now().UnixMilli()); err != nil {
				return fmt.Errorf("release retired installation: %w", err)
			}
		}
		worked = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("commit BIOS retirement: %w", err)
	}
	return worked, nil
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
	count := 0
	err := service.repository.WithRetirement(ctx, func(scope RetirementScope) error {
		now := service.now().UnixMilli()
		before, err := scope.Read.Launch(ctx, now, retirementBatchSize)
		if err != nil {
			return fmt.Errorf("read retiring launch: %w", err)
		}
		if !before.Found {
			return nil
		}
		if !validRetirement(before, now) {
			return ErrRetirementSnapshotChanged
		}
		if err := scope.Launch.FenceLaunch(ctx, before); err != nil {
			return fmt.Errorf("fence retiring launch: %w", err)
		}
		end := LaunchRetirementEnd{Before: before, State: before.State, PlayState: "ABANDONED", NowMS: now}
		end.Expire = before.State == "CREATED" || before.State == "ACTIVE"
		if end.Expire {
			end.State = "EXPIRED"
		}
		if err := scope.Launch.TerminateLaunch(ctx, end); err != nil {
			return fmt.Errorf("terminate retiring launch: %w", err)
		}
		if err := scope.Launch.ReleaseLaunchFiles(ctx, before); err != nil {
			return fmt.Errorf("release launch files: %w", err)
		}
		if len(before.Content) < retirementBatchSize && len(before.External) < retirementBatchSize {
			due := before.DueMS
			if end.Expire {
				due = now
			}
			if err := scope.Launch.CompleteLaunch(ctx, RetirementCompletion{ID: before.ID, DueMS: due, NowMS: now}); err != nil {
				return fmt.Errorf("complete launch retirement: %w", err)
			}
		}
		count = 1
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("commit launch retirement: %w", err)
	}
	return count, nil
}

func validRetirement(before LaunchRetirement, now int64) bool {
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
