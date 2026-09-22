package sourceimport

import (
	"retrom/internal/persistence/dberrors"
	repository "retrom/internal/persistence/sourceimport"
	application "retrom/internal/service/sourceimport"
)

func (service *Service) importExecutor(root Root) *application.ImportExecutor {
	return application.NewImportExecutor(application.ImportExecutorDependencies{
		Items:     application.NewItemWork(repository.NewItemWork(service.database), service.now),
		Materials: service.materialization(), Sources: importSource{service: service.sources(), root: root},
		Reviews:     service.reviewPreparation(),
		Companions:  application.NewCompanions(repository.NewCompanions(service.database), service.now),
		Settlement:  service.workerSettlement(),
		Completion:  application.NewCompletion(repository.NewCompletion(service.database), service.now),
		Diagnostics: importDiagnostics{service},
	})
}

type importDiagnostics struct{ service *Service }

func (diagnostics importDiagnostics) Sanitize(err error) string {
	return diagnostics.service.sanitizeTechnicalDetail(err)
}

func (importDiagnostics) DatabaseCause(err error) string { return dberrors.Classify(err) }
