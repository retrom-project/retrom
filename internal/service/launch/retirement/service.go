package retirement

import (
	"context"
	"fmt"
	"math"
	"time"

	jobs "retrom/internal/service/cleanupjobs"
)

const retirementBatchSize = 200

type Service struct {
	launch jobs.LaunchRetirementRepository
	now    func() time.Time
}

func New(repository jobs.LaunchRetirementRepository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{launch: repository, now: now}
}

func (service *Service) Launches(ctx context.Context) error {
	for {
		count, err := service.LaunchBatch(ctx)
		if err != nil || count == 0 {
			return err
		}
	}
}

func (service *Service) LaunchBatch(ctx context.Context) (int, error) {
	count := 0
	err := service.launch.WithLaunchRetirement(ctx, func(scope jobs.LaunchRetirementScope) error {
		count = 0
		now := service.now().UnixMilli()
		before, err := scope.Read.Launch(ctx, now, retirementBatchSize)
		if err != nil {
			return fmt.Errorf("read retiring launch: %w", err)
		}
		if !before.Found {
			return nil
		}
		if !validRetirement(before, now) {
			return jobs.ErrRetirementSnapshotChanged
		}
		if err := scope.Launch.FenceLaunch(ctx, before); err != nil {
			return fmt.Errorf("fence retiring launch: %w", err)
		}
		end := jobs.LaunchRetirementEnd{
			Before:    before,
			State:     before.State,
			PlayState: "ABANDONED",
			NowMS:     now,
		}
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
			if err := scope.Launch.CompleteLaunch(
				ctx,
				jobs.RetirementCompletion{ID: before.ID, DueMS: due, NowMS: now},
			); err != nil {
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

func validRetirement(before jobs.LaunchRetirement, now int64) bool {
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
		return before.HardMS == before.DueMS
	case "FINISHED", "EXPIRED", "REVOKED":
		return before.Finished.Set && before.Finished.Value == before.DueMS
	default:
		return false
	}
}
