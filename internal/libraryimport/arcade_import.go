package libraryimport

import (
	"context"
	"database/sql"
)

func (service *Service) prepareArcadeFiles(
	ctx context.Context,
	files []importSourceFile,
	datID sql.NullString,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	dispositions, groups, archives, err := service.preparation.PrepareArcadeFiles(ctx, files, datID.String)
	return dispositions, groups, archives, legacyPreparationError(err)
}
