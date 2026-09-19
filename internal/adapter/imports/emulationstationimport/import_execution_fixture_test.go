package emulationstationimport

import (
	"context"

	blobmodel "retrom/internal/model/blob"
	emulationstationimportmodel "retrom/internal/model/emulationstationimport"

	"retrom/internal/foundation/cleanup"
	persistence "retrom/internal/repo/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
)

func (service *Service) processItem(ctx context.Context, unit work, _ Root, item executionItem) error {
	return service.importExecutor().Process(ctx, unit, item)
}

func (service *Service) nextItem(ctx context.Context, unit work) (executionItem, bool, error) {
	return service.itemWork().Next(ctx, unit)
}

func (service *Service) copyExecutionFiles(ctx context.Context, unit work, _ Root, item *executionItem) bool {
	copied, err := service.importExecutor().CopyFiles(ctx, unit, item)
	if err != nil {
		cleanup.Error("copy EmulationStation fixture source", err)
	}
	return copied && err == nil
}

func fileMaterial(itemID string, file executionFile) emulationstationimportmodel.MaterialSource {
	return emulationstationimportmodel.MaterialSource{
		Key:   emulationstationimportmodel.MaterialKey{ItemID: itemID, Ordinal: file.Ordinal},
		Path:  file.Path,
		Facts: file.Facts,
		Size:  file.Size,
	}
}

func (service *Service) recordCopiedFile(
	ctx context.Context,
	unit work,
	itemID string,
	file executionFile,
	metadata blobmodel.PreparedBlob,
) (string, error) {
	return service.materialization().Copy(ctx, unit, fileMaterial(itemID, file), verifiedBlob(metadata))
}

func (service *Service) finishImport(ctx context.Context, unit work) error {
	return application.NewCompletion(persistence.NewCompletion(service.database), service.now).Finish(ctx, unit)
}
