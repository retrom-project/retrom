package payloadrelease

import (
	"context"
	"fmt"

	application "retrom/internal/model/payloadrelease"
)

func (service *Service) StageInScope(ctx context.Context, scope application.GCScope, ids []string) error {
	if err := service.gc.StageInScope(ctx, scope, ids); err != nil {
		return fmt.Errorf("stage payload release scope: %w", err)
	}
	return nil
}
