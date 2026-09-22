package sourceimport

import (
	"log/slog"

	repository "retrom/internal/persistence/sourceimport"
	application "retrom/internal/service/sourceimport"
)

func (service *Service) dispatcher() *application.WorkDispatcher {
	return application.NewWorkDispatcher(application.WorkDispatchDependencies{
		Sources: service.sources(), Publication: service.scanPublication(), Settlement: service.workerSettlement(),
		Import: application.ImportExecutorDependencies{
			Items: application.NewItemWork(repository.NewItemWork(service.database), service.now), Materials: service.materialization(), Reviews: service.reviewPreparation(),
			Companions: application.NewCompanions(repository.NewCompanions(service.database), service.now), Settlement: service.workerSettlement(), Completion: application.NewCompletion(repository.NewCompletion(service.database), service.now), Diagnostics: importDiagnostics{service},
		}, Report: func(err error) {
			slog.Error("Source fixture worker failed", "error", service.sanitizeTechnicalDetail(err))
		},
	})
}
