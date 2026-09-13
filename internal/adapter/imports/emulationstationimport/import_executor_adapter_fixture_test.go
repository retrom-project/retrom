package emulationstationimport

import (
	"context"

	"retrom/internal/repo/dberrors"
	persistence "retrom/internal/repo/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
)

type importExecutorAdapter struct{ service *Service }

func (service *Service) importExecutor() *application.ImportExecutor {
	adapter := importExecutorAdapter{service: service}
	return application.NewImportExecutor(
		application.ImportExecutorDependencies{
			Items:       service.itemWork(),
			Materials:   service.materialization(),
			Sources:     adapter,
			Reviews:     service.reviewPreparer(),
			Control:     service.executionControl(),
			Completion:  application.NewCompletion(persistence.NewCompletion(service.database), service.now),
			Diagnostics: adapter,
		},
	)
}

func (adapter importExecutorAdapter) CopyFile(
	ctx context.Context,
	unit work,
	file executionFile,
) (application.VerifiedBlob, error) {
	return adapter.service.sources().CopyFile(ctx, unit, file)
}

func (adapter importExecutorAdapter) CopyAsset(
	ctx context.Context,
	unit work,
	asset executionAsset,
) (application.VerifiedBlob, bool, error) {
	return adapter.service.sources().CopyAsset(ctx, unit, asset)
}

func (adapter importExecutorAdapter) Sanitize(err error) string {
	return adapter.service.sanitizeTechnicalDetail(err)
}
func (importExecutorAdapter) DatabaseCause(err error) string { return dberrors.Classify(err) }
