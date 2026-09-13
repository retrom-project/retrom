package payloadrelease

import (
	"context"
	"fmt"
)

func (service *Service) releaseExpiredProviderPayloads(ctx context.Context) error {
	if err := service.expirations.Providers(ctx); err != nil {
		return fmt.Errorf("expire provider payloads: %w", err)
	}
	return nil
}

func (service *Service) releaseExpiredProviderPayloadBatch(ctx context.Context) (int, error) {
	count, err := service.expirations.ProviderBatch(ctx)
	if err != nil {
		return 0, fmt.Errorf("expire provider payload batch: %w", err)
	}
	return count, nil
}
