package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/blobstore"
	"retrom/internal/contentprofile"
	"retrom/internal/importing"
	"retrom/internal/rpgmaker/fileset"
	"retrom/internal/scummvm"
)

func (service *Service) WithScummVMDetector(detector *scummvm.Detector) *Service {
	service.scummVMDetector = detector
	return service
}

func (service *Service) prepareScummVMProject(
	ctx context.Context,
	sourceType string,
	files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	if service.scummVMDetector == nil {
		return nil, nil, nil, scummvm.ErrToolFailed
	}
	return service.prepareProject(ctx, sourceType, files, func(
		files []importSourceFile,
	) ([]preparedDisposition, preparedGroup, error) {
		return service.prepareScummVMDirectory(ctx, files)
	}, service.prepareScummVMArchive)
}

func (service *Service) prepareScummVMDirectory(
	ctx context.Context,
	files []importSourceFile,
) ([]preparedDisposition, preparedGroup, error) {
	project, err := fileset.NormalizeTree(directoryProjectInput(files))
	if err != nil {
		return nil, preparedGroup{}, fmt.Errorf("normalize ScummVM directory: %w", err)
	}
	metadata := make(map[int]blobstore.Metadata, len(project.Files))
	for _, file := range project.Files {
		source := files[file.SourceIndex]
		metadata[file.SourceIndex] = blobstore.Metadata{
			Path: service.blobs.Path(source.sha256), SHA256: source.sha256, Size: source.size,
		}
	}
	snapshot, err := service.detectScummVMTree(ctx, project.Files, metadata, nil)
	if err != nil {
		return nil, preparedGroup{}, err
	}
	dispositions, sources := directoryProjectSources(files, project.Files)
	group, err := newScummVMGroup(sources, snapshot, rpgMakerDirectoryTitle(files))
	return dispositions, group, err
}

func (service *Service) prepareScummVMArchive(
	ctx context.Context,
	file importSourceFile,
) (preparedDisposition, preparedGroup, preparedArchive, error) {
	format, reason := profileArchiveFormat(file.path)
	if reason != "" || format != contentprofile.ArchiveZIP && format != contentprofile.ArchiveSevenZip {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, ErrInvalid
	}
	entries, candidates, err := service.scanProjectArchive(ctx, file, format)
	if err != nil {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, err
	}
	defer func() { discardProjectArchiveCandidates(candidates) }()
	input := make([]fileset.SourceFile, 0, len(entries))
	entryByOrdinal := make(map[int]importing.ArchiveEntry, len(entries))
	for _, entry := range entries {
		input = append(input, fileset.SourceFile{
			Path: entry.NormalizedPath, SizeBytes: entry.Size, SourceIndex: entry.Ordinal,
		})
		entryByOrdinal[entry.Ordinal] = entry
	}
	project, err := fileset.NormalizeTree(input)
	if err != nil {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, fmt.Errorf("normalize ScummVM archive: %w", err)
	}
	selected := archiveProjectEntries(project.Files, entryByOrdinal)
	metadata, err := service.projectArchiveReadMetadata(ctx, file, selected, candidates)
	if err != nil {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, err
	}
	snapshot, err := service.detectScummVMTree(ctx, project.Files, metadata, &file)
	if err != nil {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, err
	}
	materialized, err := projectArchiveMaterialization(selected, candidates, metadata)
	if err != nil {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, err
	}
	group, err := newScummVMGroup(archiveProjectSources(file, project.Files), snapshot, file.path)
	archive := preparedArchive{blobID: file.blobID, entries: entries, materialized: materialized}
	return sourceDisposition(file), group, archive, err
}

func newScummVMGroup(sources []preparedSource, snapshot scummvm.Snapshot, title string) (preparedGroup, error) {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return preparedGroup{}, fmt.Errorf("encode ScummVM detection: %w", err)
	}
	status, code := snapshot.Status()
	sortPreparedSources(sources)
	return preparedGroup{
		sources: sources, contentKind: scummvm.ContentKind, validationStatus: status,
		compatibilityCode: code, dependencySnapshot: string(encoded), titleSource: title, titleSourceExplicit: true,
	}, nil
}
