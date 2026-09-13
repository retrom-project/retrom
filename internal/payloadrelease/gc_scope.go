package payloadrelease

import (
	"context"

	application "retrom/internal/service/payloadrelease"
)

func (service *Service) StageInScope(ctx context.Context, scope application.GCScope, ids []string) error {
	return service.gc.StageInScope(ctx, scope, ids)
}
