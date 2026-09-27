package providerretention

import (
	"context"
	"fmt"
	"time"

	jobs "retrom/internal/service/cleanupjobs"
)

const expirationBatchSize = 200

type Service struct {
	providers jobs.ProviderExpirationRepository
	deletion  jobs.DeletionStager
	now       func() time.Time
}

func New(repository jobs.ProviderExpirationRepository, deletion jobs.DeletionStager, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{providers: repository, deletion: deletion, now: now}
}

func (service *Service) Providers(ctx context.Context) error {
	for {
		count, err := service.ProviderBatch(ctx)
		if err != nil || count < expirationBatchSize {
			return err
		}
	}
}

func (service *Service) ProviderBatch(ctx context.Context) (int, error) {
	count := 0
	err := service.providers.WithProviderExpiration(ctx, func(scope jobs.ProviderExpirationScope) error {
		now := service.now().UnixMilli()
		responses, err := scope.Read.Providers(ctx, now, expirationBatchSize)
		if err != nil {
			return fmt.Errorf("read expired provider payloads: %w", err)
		}
		var blobs []string
		for _, before := range responses {
			if before.ID == "" || before.State != "RETAINED" || before.BlobID == "" ||
				before.Running || before.ExpiresMS > now || before.CacheCount < 0 {
				return jobs.ErrExpirationSnapshotChanged
			}
			if err := scope.Write.ReleaseProvider(ctx, before, now); err != nil {
				return fmt.Errorf("release expired provider payload: %w", err)
			}
			blobs = append(blobs, before.BlobID)
		}
		if err := service.deletion.StageInScope(ctx, scope.DeletionQueue, blobs); err != nil {
			return fmt.Errorf("stage expired provider payloads: %w", err)
		}
		count = len(responses)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("commit provider expiration: %w", err)
	}
	return count, nil
}
