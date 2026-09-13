package libraryimport

import (
	"context"
	"database/sql"
)

func (service *Service) prepareImportFiles(ctx context.Context, platformID, sourceType string, files []importSourceFile,
	datID sql.NullString,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	dispositions, groups, archives, err := service.importPreparation().PrepareImportFiles(
		ctx, platformID, sourceType, files, datID.String,
	)
	return dispositions, groups, archives, legacyPreparationError(err)
}
