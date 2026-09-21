package payloadrelease

import (
	"context"
	"fmt"
)

func (service *Service) releaseExpiredReviewPreviews(ctx context.Context) error {
	if err := service.expirations.Previews(ctx); err != nil {
		return fmt.Errorf("expire review preview payloads: %w", err)
	}
	return nil
}
