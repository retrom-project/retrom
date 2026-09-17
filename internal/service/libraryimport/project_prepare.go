package libraryimport

import (
	"context"
	model "retrom/internal/model/libraryimport"
)

type projectDirectoryPreparer func(
	[]model.ImportFile,
) ([]model.PreparedDisposition, model.PreparedGroup, error)

type projectArchivePreparer func(
	context.Context,
	model.ImportFile,
) (model.PreparedDisposition, model.PreparedGroup, model.PreparedArchive, error)

func (service *ImportPreparation) prepareProject(
	ctx context.Context,
	sourceType string,
	files []model.ImportFile,
	prepareDirectory projectDirectoryPreparer,
	prepareArchive projectArchivePreparer,
) ([]model.PreparedDisposition, []model.PreparedGroup, []model.PreparedArchive, error) {
	if service.blobs == nil {
		return nil, nil, nil, model.ErrInvalid
	}
	if sourceType == "DIRECTORY" {
		dispositions, group, err := prepareDirectory(files)
		if err != nil {
			return nil, nil, nil, err
		}
		return dispositions, []model.PreparedGroup{group}, nil, nil
	}
	if sourceType != "FILES" || len(files) != 1 {
		return nil, nil, nil, model.ErrInvalid
	}
	disposition, group, archive, err := prepareArchive(ctx, files[0])
	if err != nil {
		return nil, nil, nil, err
	}
	return []model.PreparedDisposition{disposition}, []model.PreparedGroup{group}, []model.PreparedArchive{archive}, nil
}
