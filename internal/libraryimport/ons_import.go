package libraryimport

import (
	"context"
	"fmt"
	"io"
	"os"

	"retrom/internal/contentprofile"
	"retrom/internal/importing"
	"retrom/internal/ons/detector"
	"retrom/internal/rpgmaker/fileset"
)

type onsProjectIndex struct {
	files []detector.File
	paths map[string]string
}

func (index onsProjectIndex) Files() []detector.File {
	return append([]detector.File(nil), index.files...)
}

func (index onsProjectIndex) Open(logicalPath string) (io.ReadCloser, error) {
	localPath, exists := index.paths[logicalPath]
	if !exists {
		return nil, os.ErrNotExist
	}
	reader, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("open ONS project file: %w", err)
	}
	return reader, nil
}

func (service *Service) prepareONSProject(
	ctx context.Context,
	sourceType string,
	files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	if service.blobs == nil {
		return nil, nil, nil, ErrInvalid
	}
	if sourceType == "DIRECTORY" {
		dispositions, group, err := service.prepareONSDirectory(files)
		if err != nil {
			return nil, nil, nil, err
		}
		return dispositions, []preparedGroup{group}, nil, nil
	}
	if sourceType != "FILES" || len(files) != 1 {
		return nil, nil, nil, ErrInvalid
	}
	disposition, group, archive, err := service.prepareONSArchive(ctx, files[0])
	if err != nil {
		return nil, nil, nil, err
	}
	return []preparedDisposition{disposition}, []preparedGroup{group}, []preparedArchive{archive}, nil
}

func (service *Service) prepareONSDirectory(
	files []importSourceFile,
) ([]preparedDisposition, preparedGroup, error) {
	input := make([]fileset.SourceFile, 0, len(files))
	for index, file := range files {
		input = append(input, fileset.SourceFile{Path: file.path, SizeBytes: file.size, SourceIndex: index})
	}
	project, err := fileset.NormalizeProjectWithMarkers(input, detector.Markers())
	if err != nil {
		return nil, preparedGroup{}, fmt.Errorf("normalize ONS directory: %w", err)
	}
	index := onsProjectIndex{paths: make(map[string]string, len(project.Files))}
	for _, file := range project.Files {
		source := files[file.SourceIndex]
		index.files = append(index.files, detector.File{Path: file.Path, Size: file.SizeBytes})
		index.paths[file.Path] = service.blobs.Path(source.sha256)
	}
	profile, err := detector.Detect(index)
	if err != nil {
		return nil, preparedGroup{}, fmt.Errorf("detect ONS directory: %w", err)
	}
	dispositions := make([]preparedDisposition, 0, len(files))
	sources := make([]preparedSource, 0, len(project.Files))
	included := make(map[int]fileset.SourceFile, len(project.Files))
	for _, file := range project.Files {
		included[file.SourceIndex] = file
	}
	for sourceIndex, source := range files {
		file, exists := included[sourceIndex]
		if !exists {
			dispositions = append(dispositions, preparedDisposition{
				file: source, disposition: "IGNORED", reason: "IGNORED_SYSTEM_SIDECAR",
			})
			continue
		}
		dispositions = append(dispositions, sourceDisposition(source))
		sources = append(sources, preparedSource{file: source, role: "PROJECT_FILE", logicalName: file.Path})
	}
	return dispositions, newONSGroup(sources, profile, rpgMakerDirectoryTitle(files)), nil
}

func (service *Service) prepareONSArchive(
	ctx context.Context,
	file importSourceFile,
) (preparedDisposition, preparedGroup, preparedArchive, error) {
	archiveFormat, reason := profileArchiveFormat(file.path)
	if reason != "" {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, ErrInvalid
	}
	entries, err := service.scanRPGMakerArchive(ctx, file, archiveFormat)
	if err != nil {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, err
	}
	input := make([]fileset.SourceFile, 0, len(entries))
	entryByOrdinal := make(map[int]importing.ArchiveEntry, len(entries))
	for _, entry := range entries {
		input = append(input, fileset.SourceFile{
			Path: entry.NormalizedPath, SizeBytes: entry.Size, SourceIndex: entry.Ordinal,
		})
		entryByOrdinal[entry.Ordinal] = entry
	}
	project, err := fileset.NormalizeProjectWithMarkers(input, detector.Markers())
	if err != nil {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, fmt.Errorf("normalize ONS archive: %w", err)
	}
	projectEntries := make([]importing.ArchiveEntry, 0, len(project.Files))
	for _, projectFile := range project.Files {
		projectEntries = append(projectEntries, entryByOrdinal[projectFile.SourceIndex])
	}
	materialized, err := service.materializeArchiveEntries(
		ctx, service.blobs.Path(file.sha256), projectEntries,
	)
	if err != nil {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, err
	}
	index := onsProjectIndex{paths: make(map[string]string, len(project.Files))}
	for _, projectFile := range project.Files {
		entry := entryByOrdinal[projectFile.SourceIndex]
		index.files = append(index.files, detector.File{Path: projectFile.Path, Size: projectFile.SizeBytes})
		index.paths[projectFile.Path] = materialized[entry.Ordinal].Path
	}
	profile, err := detector.Detect(index)
	if err != nil {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, fmt.Errorf("detect ONS archive: %w", err)
	}
	sources := make([]preparedSource, 0, len(project.Files))
	for _, projectFile := range project.Files {
		ordinal := projectFile.SourceIndex
		sources = append(sources, preparedSource{
			file: file, role: "PROJECT_FILE", logicalName: projectFile.Path,
			archiveBlobID: file.blobID, archiveOrdinal: &ordinal,
		})
	}
	return sourceDisposition(file), newONSGroup(sources, profile, file.path), preparedArchive{
		blobID: file.blobID, entries: entries, materialized: materialized,
	}, nil
}

func newONSGroup(sources []preparedSource, profile detector.Profile, titleSource string) preparedGroup {
	sortPreparedSources(sources)
	profileJSON, _ := detector.MarshalSnapshot(profile)
	return preparedGroup{
		sources: sources, contentKind: string(contentprofile.ContentKindONSProject),
		validationStatus: "BLOCKED", compatibilityCode: "ONS_RUNTIME_TRIAL_REQUIRED",
		dependencySnapshot: string(profileJSON), titleSource: titleSource, titleSourceExplicit: true,
	}
}
