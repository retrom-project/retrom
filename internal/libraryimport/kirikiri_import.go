package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/contentprofile"
	"retrom/internal/importing"
	"retrom/internal/kirikiri/detector"
	"retrom/internal/rpgmaker/fileset"
)

type kirikiriProjectIndex struct{ files []detector.File }

func (index kirikiriProjectIndex) Files() []detector.File {
	return append([]detector.File(nil), index.files...)
}

func (service *Service) prepareKiriKiriProject(
	ctx context.Context,
	sourceType string,
	files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	return service.prepareProject(
		ctx, sourceType, files, service.prepareKiriKiriDirectory, service.prepareKiriKiriArchive,
	)
}

func (service *Service) prepareKiriKiriDirectory(
	files []importSourceFile,
) ([]preparedDisposition, preparedGroup, error) {
	input := make([]fileset.SourceFile, 0, len(files))
	for index, file := range files {
		input = append(input, fileset.SourceFile{Path: file.path, SizeBytes: file.size, SourceIndex: index})
	}
	project, err := fileset.NormalizeProjectWithMarkers(input, detector.Markers())
	if err != nil {
		return nil, preparedGroup{}, fmt.Errorf("normalize KiriKiri directory: %w", err)
	}
	index := kirikiriProjectIndex{files: make([]detector.File, 0, len(project.Files))}
	for _, file := range project.Files {
		index.files = append(index.files, detector.File{Path: file.Path, Size: file.SizeBytes})
	}
	profile, err := detector.Detect(index)
	if err != nil {
		return nil, preparedGroup{}, fmt.Errorf("detect KiriKiri directory: %w", err)
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
	return dispositions, newKiriKiriGroup(sources, profile, rpgMakerDirectoryTitle(files)), nil
}

func (service *Service) prepareKiriKiriArchive(
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
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, fmt.Errorf("normalize KiriKiri archive: %w", err)
	}
	projectEntries := make([]importing.ArchiveEntry, 0, len(project.Files))
	index := kirikiriProjectIndex{files: make([]detector.File, 0, len(project.Files))}
	for _, projectFile := range project.Files {
		projectEntries = append(projectEntries, entryByOrdinal[projectFile.SourceIndex])
		index.files = append(index.files, detector.File{Path: projectFile.Path, Size: projectFile.SizeBytes})
	}
	materialized, err := service.materializeArchiveEntries(ctx, service.blobs.Path(file.sha256), projectEntries)
	if err != nil {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, err
	}
	profile, err := detector.Detect(index)
	if err != nil {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, fmt.Errorf("detect KiriKiri archive: %w", err)
	}
	sources := make([]preparedSource, 0, len(project.Files))
	for _, projectFile := range project.Files {
		ordinal := projectFile.SourceIndex
		sources = append(sources, preparedSource{
			file: file, role: "PROJECT_FILE", logicalName: projectFile.Path,
			archiveBlobID: file.blobID, archiveOrdinal: &ordinal,
		})
	}
	return sourceDisposition(file), newKiriKiriGroup(sources, profile, file.path), preparedArchive{
		blobID: file.blobID, entries: entries, materialized: materialized,
	}, nil
}

func newKiriKiriGroup(sources []preparedSource, profile detector.Profile, titleSource string) preparedGroup {
	sortPreparedSources(sources)
	profileJSON, _ := detector.MarshalSnapshot(profile)
	return preparedGroup{
		sources: sources, contentKind: string(contentprofile.ContentKindKiriKiriProject),
		validationStatus: "BLOCKED", compatibilityCode: "KIRIKIRI_RUNTIME_TRIAL_REQUIRED",
		dependencySnapshot: string(profileJSON), titleSource: titleSource, titleSourceExplicit: true,
	}
}
