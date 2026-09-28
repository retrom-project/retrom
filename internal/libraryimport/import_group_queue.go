package libraryimport

import (
	"context"
	"fmt"

	libraryservice "retrom/internal/service/libraryimport"
)

// QueueCreate admits work using the same queue and worker as the HTTP entry point.
func (service *Service) QueueCreate(ctx context.Context, request CreateRequest) (Created, error) {
	result, err := service.admissions.Queue(ctx, request)
	if err != nil {
		return Created{}, fmt.Errorf("queue import: %w", err)
	}
	return result, nil
}

func targetGuard(target creationTarget) libraryservice.ImportTargetGuard {
	return libraryservice.TargetImportGuard(importTargetFacts(target))
}
