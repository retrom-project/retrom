package payloadrelease

import (
	"context"
	"fmt"

	payloadreleasemodel "retrom/internal/model/payloadrelease"
)

func (service *Service) StageInScope(ctx context.Context, scope payloadreleasemodel.GCScope, ids []string) error {
	if err := service.gc.StageInScope(ctx, scope, ids); err != nil {
		return fmt.Errorf("stage payload release scope: %w", err)
	}
	return nil
}
