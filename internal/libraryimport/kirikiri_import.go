package libraryimport

import (
	"context"
)

func (service *Service) prepareKiriKiriProject(
	ctx context.Context, sourceType string, files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	dispositions, groups, archives, err := service.importPreparation().PrepareKiriKiriProject(ctx, sourceType, files)
	return dispositions, groups, archives, legacyPreparationError(err)
}
