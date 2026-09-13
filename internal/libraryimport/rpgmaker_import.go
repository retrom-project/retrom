package libraryimport

import (
	"context"

	application "retrom/internal/service/libraryimport"
)

func (service *Service) prepareRPGMakerProject(
	ctx context.Context, sourceType string, files []importSourceFile, coreID string,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	dispositions, groups, archives, err := service.importPreparation().PrepareRPGMakerProject(
		ctx, sourceType, files, coreID,
	)
	return dispositions, groups, archives, legacyPreparationError(err)
}

func rpgMakerDirectoryTitle(files []importSourceFile) string {
	return application.RpgMakerDirectoryTitle(files)
}
