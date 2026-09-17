package libraryimport

import (
	"context"
	"fmt"

	libraryimportmodel "retrom/internal/model/libraryimport"
	repository "retrom/internal/repo/libraryimport"
	libraryimportservice "retrom/internal/service/libraryimport"
)

// QueueCreate is the compatibility entry to the import admission use case.
func (service *Service) QueueCreate(ctx context.Context, request CreateRequest) (Created, error) {
	admissions := libraryimportservice.NewImportAdmissions(
		repository.NewImportAdmissions(service.database),
		service,
		service.tags,
		libraryimportmodel.ImportAdmissionOptions{
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

func targetGuard(target creationTarget) libraryimportmodel.ImportTargetGuard {
	return libraryimportservice.TargetImportGuard(importTargetFacts(target))
}
