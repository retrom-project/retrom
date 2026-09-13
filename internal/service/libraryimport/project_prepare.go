package libraryimport

import "context"

type projectDirectoryPreparer func(
	[]ImportFile,
) ([]PreparedDisposition, PreparedGroup, error)

type projectArchivePreparer func(
	context.Context,
	ImportFile,
) (PreparedDisposition, PreparedGroup, PreparedArchive, error)

func (service *ImportPreparation) prepareProject(
	ctx context.Context,
	sourceType string,
	files []ImportFile,
	prepareDirectory projectDirectoryPreparer,
	prepareArchive projectArchivePreparer,
) ([]PreparedDisposition, []PreparedGroup, []PreparedArchive, error) {
	if service.blobs == nil {
		return nil, nil, nil, ErrInvalid
	}
	if sourceType == "DIRECTORY" {
		dispositions, group, err := prepareDirectory(files)
		if err != nil {
			return nil, nil, nil, err
		}
		return dispositions, []PreparedGroup{group}, nil, nil
	}
	if sourceType != "FILES" || len(files) != 1 {
		return nil, nil, nil, ErrInvalid
	}
	disposition, group, archive, err := prepareArchive(ctx, files[0])
	if err != nil {
		return nil, nil, nil, err
	}
	return []PreparedDisposition{disposition}, []PreparedGroup{group}, []PreparedArchive{archive}, nil
}
