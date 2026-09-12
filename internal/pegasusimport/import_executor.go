package pegasusimport

import (
	"context"
	"fmt"
	"log/slog"

	"retrom/internal/persistence/dberrors"
	repository "retrom/internal/persistence/pegasusimport"
	application "retrom/internal/service/pegasusimport"
)

func (service *Service) importExecutor(root Root) *application.ImportExecutor {
	return application.NewImportExecutor(application.ImportExecutorDependencies{
		Items:     application.NewItemWork(repository.NewItemWork(service.database), service.now),
		Materials: service.materialization(), Sources: importSource{service: service, root: root},
		Reviews:     service.reviewPreparation(),
		Companions:  application.NewCompanions(repository.NewCompanions(service.database), service.now),
		Settlement:  service.workerSettlement(),
		Completion:  application.NewCompletion(repository.NewCompletion(service.database), service.now),
		Diagnostics: importDiagnostics{service},
	})
}

func (service *Service) executeImport(ctx context.Context, unit work, root Root) {
	if err := service.importExecutor(root).Execute(ctx, unit); err != nil && ctx.Err() == nil {
		slog.Error("Pegasus import execution stopped", "error", service.sanitizeTechnicalDetail(err))
	}
}

type importSource struct {
	service *Service
	root    Root
}

func (source importSource) CopyFile(
	ctx context.Context, unit application.Work, file application.ExecutionFile,
) (application.VerifiedBlob, error) {
	blob, err := source.service.copySource(ctx, source.root, unit.RelativePath, file.Path, file.Size, file.Facts)
	if err != nil {
		return application.VerifiedBlob{}, fmt.Errorf("read Pegasus import file: %w", err)
	}
	return verifiedMaterial(blob), nil
}

func (source importSource) CopyAsset(
	ctx context.Context, unit application.Work, asset application.ExecutionAsset,
) (application.VerifiedBlob, bool, error) {
	blob, valid, err := source.service.copyAsset(ctx, source.root, unit.RelativePath, asset)
	if err != nil {
		return application.VerifiedBlob{}, valid, fmt.Errorf("read Pegasus import asset: %w", err)
	}
	return verifiedMaterial(blob), valid, nil
}

type importDiagnostics struct{ service *Service }

func (diagnostics importDiagnostics) Sanitize(err error) string {
	return diagnostics.service.sanitizeTechnicalDetail(err)
}

func (importDiagnostics) DatabaseCause(err error) string { return dberrors.Classify(err) }
