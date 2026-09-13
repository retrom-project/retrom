package emulationstationimport

import (
	"context"

	"retrom/internal/persistence/dberrors"
	persistence "retrom/internal/persistence/emulationstationimport"
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

func (adapter importExecutorAdapter) root(unit work) (Root, error) {
	root, found := adapter.service.roots[unit.RootID]
	if !found || root.digest != unit.RootDigest {
		return Root{}, ErrSourceChanged
	}
	return root, nil
}

func (adapter importExecutorAdapter) CopyFile(
	ctx context.Context,
	unit work,
	file executionFile,
) (application.VerifiedBlob, error) {
	root, err := adapter.root(unit)
	if err != nil {
		return application.VerifiedBlob{}, err
	}
	metadata, err := adapter.service.copySource(ctx, root, unit, unit.RelativePath, file.Path, file.Size, file.Facts)
	if err != nil {
		return application.VerifiedBlob{}, err
	}
	return verifiedBlob(metadata), nil
}

func (adapter importExecutorAdapter) CopyAsset(
	ctx context.Context,
	unit work,
	asset executionAsset,
) (application.VerifiedBlob, bool, error) {
	root, err := adapter.root(unit)
	if err != nil {
		return application.VerifiedBlob{}, false, err
	}
	metadata, valid, err := adapter.service.copyAsset(ctx, root, unit, unit.RelativePath, asset)
	if err != nil {
		return application.VerifiedBlob{}, false, err
	}
	return verifiedBlob(metadata), valid, nil
}

func (adapter importExecutorAdapter) Sanitize(err error) string {
	return adapter.service.sanitizeTechnicalDetail(err)
}
func (importExecutorAdapter) DatabaseCause(err error) string { return dberrors.Classify(err) }
