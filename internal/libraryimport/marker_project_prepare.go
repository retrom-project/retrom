package libraryimport

import (
	"context"

	"retrom/internal/core/rpgmaker/fileset"
	"retrom/internal/filestore"
	libraryservice "retrom/internal/service/libraryimport"
)

func (service *Service) prepareButterscotchProject(ctx context.Context, sourceType string,
	files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	dispositions, groups, archives, err := service.preparation.
		PrepareButterscotchProject(ctx, sourceType, files)
	return dispositions, groups, archives, legacyPreparationError(err)
}

func (service *Service) prepareONSProject(
	ctx context.Context,
	sourceType string,
	files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	dispositions, groups, archives, err := service.preparation.PrepareONSProject(ctx, sourceType, files)
	return dispositions, groups, archives, legacyPreparationError(err)
}

func (service *Service) prepareTyranoScriptProject(
	ctx context.Context,
	sourceType string,
	files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	dispositions, groups, archives, err := service.preparation.
		PrepareTyranoScriptProject(ctx, sourceType, files)
	return dispositions, groups, archives, legacyPreparationError(err)
}

func archiveProjectPaths(
	files []fileset.SourceFile,
	metadata map[int]filestore.Metadata,
) (map[int]string, error) {
	paths, err := libraryservice.ArchiveProjectPaths(files, metadata)
	return paths, legacyPreparationError(err)
}
