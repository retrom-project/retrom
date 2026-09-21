package libraryimport

import (
	"context"
	"fmt"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

// QueueCreate is the compatibility entry to the import admission use case.
func (service *Service) QueueCreate(ctx context.Context, request CreateRequest) (Created, error) {
	admissions := application.NewImportAdmissions(
		repository.NewImportAdmissions(service.database),
		service,
		service.tags,
		application.ImportAdmissionOptions{
			Now:                      service.now,
			MultiDiscEnabled:         service.multiDiscImportEnabled,
			MetadataScraperAvailable: service.scraper != nil,
		},
	)
	result, err := admissions.Queue(ctx, request)
	if err != nil {
		return Created{}, fmt.Errorf("queue import: %w", err)
	}
	return result, nil
}

func targetGuard(target creationTarget) application.ImportTargetGuard {
	return application.TargetImportGuard(importTargetFacts(target))
}
