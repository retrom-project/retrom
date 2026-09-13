package payloadrelease

import (
	"context"
	"fmt"
)

func (service *Service) releaseTerminalLaunches(ctx context.Context) error {
	if err := service.retirements.Launches(ctx); err != nil {
		return fmt.Errorf("retire launch payloads: %w", err)
	}
	return nil
}
