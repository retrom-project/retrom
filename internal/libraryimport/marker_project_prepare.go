package libraryimport

import (
	"context"

	"retrom/internal/core/rpgmaker/fileset"
	"retrom/internal/filestore"
	application "retrom/internal/service/libraryimport"
)

func (service *Service) prepareButterscotchProject(ctx context.Context, sourceType string,
	files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	dispositions, groups, archives, err := service.importPreparation().
		PrepareButterscotchProject(ctx, sourceType, files)
	return dispositions, groups, archives, legacyPreparationError(err)
}

func (service *Service) prepareONSProject(
	ctx context.Context,
	sourceType string,
	files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	dispositions, groups, archives, err := service.importPreparation().PrepareONSProject(ctx, sourceType, files)
	return dispositions, groups, archives, legacyPreparationError(err)
}

func (service *Service) prepareTyranoScriptProject(
	ctx context.Context,
	sourceType string,
	files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	dispositions, groups, archives, err := service.importPreparation().
		PrepareTyranoScriptProject(ctx, sourceType, files)
	return dispositions, groups, archives, legacyPreparationError(err)
}

func archiveProjectPaths(
	files []fileset.SourceFile,
	metadata map[int]filestore.Metadata,
) (map[int]string, error) {
	paths, err := application.ArchiveProjectPaths(files, metadata)
	return paths, legacyPreparationError(err)
}
