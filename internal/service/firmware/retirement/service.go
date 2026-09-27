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
	bios jobs.BIOSRetirementRepository
	now  func() time.Time
}

func New(repository jobs.BIOSRetirementRepository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{bios: repository, now: now}
}

func (service *Service) BIOS(ctx context.Context) error {
	for {
		worked, err := service.BIOSBatch(ctx)
		if err != nil || !worked {
			return err
		}
	}
}

func (service *Service) BIOSBatch(ctx context.Context) (bool, error) {
	worked := false
	err := service.bios.WithBIOSRetirement(ctx, func(scope jobs.BIOSRetirementScope) error {
		before, err := scope.Read.BIOS(ctx, retirementBatchSize)
		if err != nil {
			return fmt.Errorf("read retiring BIOS: %w", err)
		}
		if !before.Found {
			return nil
		}
		if before.ID == "" || before.FileRecord == "" || before.Version < 1 || before.Version == math.MaxInt64 {
			return jobs.ErrRetirementSnapshotChanged
		}
		if err := scope.BIOS.FenceBIOS(ctx, before); err != nil {
			return fmt.Errorf("fence retiring BIOS: %w", err)
		}
		{
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
