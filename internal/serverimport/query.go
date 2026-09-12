package serverimport

import (
	"context"
	"fmt"

	importpersistence "retrom/internal/persistence/serverimport"
	importservice "retrom/internal/service/serverimport"
)

func (service *Service) control() *importservice.Control {
	roots := make(map[string]string, len(service.roots))
	for id, root := range service.roots {
		roots[id] = root.digest
	}
	return importservice.NewControl(importpersistence.NewControl(service.database), roots, service.now)
}

func (service *Service) Cancel(
	ctx context.Context,
	id string,
	version int64,
	reason, actorID string,
) (Summary, bool, error) {
	result, pending, err := service.control().Cancel(ctx, id, version, reason, actorID)
	if err != nil {
		return Summary{}, false, fmt.Errorf("cancel server import: %w", err)
	}
	return result, pending, nil
}

func (service *Service) Retry(ctx context.Context, id string, version int64, actorID string) (Summary, error) {
	result, err := service.control().Retry(ctx, id, version, actorID)
	if err != nil {
		return Summary{}, fmt.Errorf("retry server import: %w", err)
	}
	service.signal()
	return result, nil
}
