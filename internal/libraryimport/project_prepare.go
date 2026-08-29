package libraryimport

import "context"

type projectDirectoryPreparer func(
	[]importSourceFile,
) ([]preparedDisposition, preparedGroup, error)

type projectArchivePreparer func(
	context.Context,
	importSourceFile,
) (preparedDisposition, preparedGroup, preparedArchive, error)

func (service *Service) prepareProject(
	ctx context.Context,
	sourceType string,
	files []importSourceFile,
	prepareDirectory projectDirectoryPreparer,
	prepareArchive projectArchivePreparer,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	if service.blobs == nil {
		return nil, nil, nil, ErrInvalid
	}
	if sourceType == "DIRECTORY" {
		dispositions, group, err := prepareDirectory(files)
		if err != nil {
			return nil, nil, nil, err
		}
		return dispositions, []preparedGroup{group}, nil, nil
	}
	if sourceType != "FILES" || len(files) != 1 {
		return nil, nil, nil, ErrInvalid
	}
	disposition, group, archive, err := prepareArchive(ctx, files[0])
	if err != nil {
		return nil, nil, nil, err
	}
	return []preparedDisposition{disposition}, []preparedGroup{group}, []preparedArchive{archive}, nil
}
