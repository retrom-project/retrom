package libraryimport

import (
	"context"
)

func (service *Service) prepareNXEngineProject(
	ctx context.Context, sourceType string, files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	dispositions, groups, archives, err := service.importPreparation().PrepareNXEngineProject(ctx, sourceType, files)
	return dispositions, groups, archives, legacyPreparationError(err)
}
