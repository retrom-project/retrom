package emulationstationimport

import (
	persistence "retrom/internal/repo/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
	library "retrom/internal/service/libraryimport"
)

func (service *Service) reviewHandoff() *application.ReviewHandoff {
	return application.NewReviewHandoff(persistence.NewReviewHandoff(service.database),
		library.NewMetadataSeeder(nil, service.now), service.now)
}

func (service *Service) reviewPreparer() *application.ReviewPreparer {
	return application.NewReviewPreparer(application.ReviewPreparerDependencies{
		Sources: service.importer, Companions: service.companions(), Items: service.itemWork(),
		Phases: service.materialization(), Handoff: service.reviewHandoff(), Diagnostics: importExecutorAdapter{
			service: service,
		},
	})
}

func (service *Service) companions() *application.Companions {
	return application.NewCompanions(
		persistence.NewCompanions(service.database),
		importExecutorAdapter{service: service},
		service.now,
	)
}
