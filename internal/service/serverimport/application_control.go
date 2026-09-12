package serverimport

import (
	"context"
	"fmt"
)

func (service *Service) Cancel(
	ctx context.Context,
	id string,
	version int64,
	reason, actorID string,
) (Summary, bool, error) {
	result, pending, err := service.control.Cancel(ctx, id, version, reason, actorID)
	if err != nil {
		return Summary{}, false, fmt.Errorf("cancel server import: %w", err)
	}
	return result, pending, nil
}

func (service *Service) Retry(ctx context.Context, id string, version int64, actorID string) (Summary, error) {
	result, err := service.control.Retry(ctx, id, version, actorID)
	if err != nil {
		return Summary{}, fmt.Errorf("retry server import: %w", err)
	}
	service.signal()
	return result, nil
}
