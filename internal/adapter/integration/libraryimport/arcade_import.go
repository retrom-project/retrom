package libraryimport

import (
	"context"
	"database/sql"

	"retrom/internal/capability/format/importing"
	libraryimportmodel "retrom/internal/model/libraryimport"
	libraryimportservice "retrom/internal/service/libraryimport"
)

func (service *Service) arcadeRequirements(ctx context.Context, datID, machine string,
) ([]arcadeROMRequirement, bool, error) {
	requirements, hasDisk, err := service.importPreparation().ArcadeRequirements(ctx, datID, machine)
	return requirements, hasDisk, legacyPreparationError(err)
}

func matchArcadeRequirements(
	entries map[string]importing.ArchiveEntry,
	requirements []arcadeROMRequirement,
) ([]string, []string, []string) {
	return libraryimportservice.MatchArcadeRequirements(entries, requirements)
}

func (service *Service) prepareArcadeFiles(
	ctx context.Context,
	files []importSourceFile,
	datID sql.NullString,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	dispositions, groups, archives, err := service.importPreparation().PrepareArcadeFiles(ctx, files, datID.String)
	return dispositions, groups, archives, legacyPreparationError(err)
}

type arcadeROMRequirement = libraryimportmodel.ArcadeROMRequirement
