package pegasusimport

import (
	repository "retrom/internal/persistence/pegasusimport"
	libraryservice "retrom/internal/service/libraryimport"
	application "retrom/internal/service/pegasusimport"

	"retrom/internal/adapter/integration/libraryimport"
)

func (service *Service) reviewPreparation() *application.ReviewPreparation {
	return application.NewReviewPreparation(service.importer,
		application.NewItemWork(repository.NewItemWork(service.database), service.now),
		application.NewReviewHandoff(repository.NewReviewHandoff(service.database),
			libraryservice.NewMetadataSeeder(nil, service.now), service.now))
}

func (service *Service) itemFailure(stage, operation string, err error, relativePath string) *FailureDetails {
	return application.DescribeFailure(importDiagnostics{service}, stage, operation, err, relativePath)
}

func (service *Service) libraryImportFailure(err error, files []libraryimport.ServerSourceFile) *FailureDetails {
	return application.LibraryFailureDetails(importDiagnostics{service}, err, files)
}
