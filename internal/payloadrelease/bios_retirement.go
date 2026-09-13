package payloadrelease

import (
	"context"
	"fmt"
)

func (service *Service) releaseSupersededBIOS(ctx context.Context) error {
	if err := service.retirements.BIOS(ctx); err != nil {
		return fmt.Errorf("retire BIOS payloads: %w", err)
	}
	return nil
}
