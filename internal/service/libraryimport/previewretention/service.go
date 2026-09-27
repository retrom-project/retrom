package previewretention

import (
	"context"
	"fmt"
	"math"
	"time"

	jobs "retrom/internal/service/cleanupjobs"
)

const expirationBatchSize = 200

type Service struct {
	previews jobs.PreviewExpirationRepository
	deletion jobs.DeletionStager
	now      func() time.Time
}

func New(repository jobs.PreviewExpirationRepository, deletion jobs.DeletionStager, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{previews: repository, deletion: deletion, now: now}
}

func (service *Service) Previews(ctx context.Context) error {
	for {
		count, err := service.PreviewBatch(ctx)
		if err != nil || count < expirationBatchSize {
			return err
		}
	}
}

func (service *Service) PreviewBatch(ctx context.Context) (int, error) {
	count := 0
	err := service.previews.WithPreviewExpiration(ctx, func(scope jobs.PreviewExpirationScope) error {
		now := service.now().UnixMilli()
		previews, err := scope.Read.Previews(ctx, now, expirationBatchSize)
		if err != nil {
			return fmt.Errorf("read expired previews: %w", err)
		}
		for _, before := range previews {
			if !previewDue(before, now) || before.ID == "" || before.Version < 1 || before.Version == math.MaxInt64 {
				return jobs.ErrExpirationSnapshotChanged
			}
			state := "EXPIRED"
			if before.State == "REVOKED" {
				state = "REVOKED"
			}
			if err := scope.Write.ExpirePreview(ctx, jobs.PreviewExpiry{Before: before, State: state, NowMS: now}); err != nil {
				return fmt.Errorf("expire review preview: %w", err)
			}
		}
		count = len(previews)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("commit preview expiration: %w", err)
	}
	return count, nil
}

func previewDue(before jobs.PreviewExpiration, now int64) bool {
	due := before.State == "CREATED" && before.BootstrapExpiresMS <= now ||
		before.HardExpiresMS <= now || before.State == "REVOKED"
	remaining := before.State != "EXPIRED" && before.State != "REVOKED" ||
		before.CheckpointFileRecord != "" || before.RestoreFileRecord != ""
	return due && remaining
}
