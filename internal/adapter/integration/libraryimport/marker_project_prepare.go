package libraryimport

import (
	"context"

	"retrom/internal/capability/engine/rpgmaker/fileset"
	application "retrom/internal/service/libraryimport"
)

func (service *Service) prepareButterscotchProject(ctx context.Context, sourceType string,
	files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	dispositions, groups, archives, err := service.importPreparation().PrepareButterscotchProject(ctx, sourceType, files)
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
	dispositions, groups, archives, err := service.importPreparation().PrepareTyranoScriptProject(ctx, sourceType, files)
	return dispositions, groups, archives, legacyPreparationError(err)
}

func archiveProjectPaths(
	files []fileset.SourceFile,
	metadata map[int]string,
) (map[int]string, error) {
	paths, err := application.ArchiveProjectPaths(files, metadata)
	return paths, legacyPreparationError(err)
}
