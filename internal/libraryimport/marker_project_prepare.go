package libraryimport

import (
	"context"
	"fmt"
	"io"
	"os"

	"retrom/internal/blobstore"
	butterscotchdetector "retrom/internal/butterscotch/detector"
	"retrom/internal/contentprofile"
	"retrom/internal/importing"
	onsdetector "retrom/internal/ons/detector"
	"retrom/internal/rpgmaker/fileset"
)

type markerProjectDefinition struct {
	name              string
	contentKind       string
	compatibilityCode string
	markers           []string
	detect            func([]fileset.SourceFile, map[int]string) ([]byte, error)
}

type localProjectPathOpener map[string]string

func (paths localProjectPathOpener) Open(logicalPath string) (io.ReadCloser, error) {
	localPath, exists := paths[logicalPath]
	if !exists {
		return nil, os.ErrNotExist
	}
	reader, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("open project file: %w", err)
	}
	return reader, nil
}

type typedProjectIndex[File any] struct {
	localProjectPathOpener
	files []File
}

func (index typedProjectIndex[File]) Files() []File {
	return append([]File(nil), index.files...)
}

var butterscotchMarkerProject = markerProjectDefinition{
	name: "Butterscotch", markers: butterscotchdetector.Markers(),
	contentKind:       string(contentprofile.ContentKindButterscotchProject),
	compatibilityCode: "BUTTERSCOTCH_RUNTIME_TRIAL_REQUIRED",
	detect: func(files []fileset.SourceFile, paths map[int]string) ([]byte, error) {
		return detectMarkerProject(
			files, paths, "Butterscotch",
			func(file fileset.SourceFile) butterscotchdetector.File {
				return butterscotchdetector.File{Path: file.Path, Size: file.SizeBytes}
			},
			func(index typedProjectIndex[butterscotchdetector.File]) (butterscotchdetector.Profile, error) {
				return butterscotchdetector.Detect(index)
			},
			butterscotchdetector.MarshalSnapshot,
		)
	},
}

var onsMarkerProject = markerProjectDefinition{
	name: "ONS", markers: onsdetector.Markers(),
	contentKind:       string(contentprofile.ContentKindONSProject),
	compatibilityCode: "ONS_RUNTIME_TRIAL_REQUIRED",
	detect: func(files []fileset.SourceFile, paths map[int]string) ([]byte, error) {
		return detectMarkerProject(
			files, paths, "ONS",
			func(file fileset.SourceFile) onsdetector.File {
				return onsdetector.File{Path: file.Path, Size: file.SizeBytes}
			},
			func(index typedProjectIndex[onsdetector.File]) (onsdetector.Profile, error) {
				return onsdetector.Detect(index)
			},
			onsdetector.MarshalSnapshot,
		)
	},
}

func (service *Service) prepareButterscotchProject(
	ctx context.Context,
	sourceType string,
	files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	return service.prepareMarkerProject(ctx, sourceType, files, butterscotchMarkerProject)
}

func (service *Service) prepareONSProject(
	ctx context.Context,
	sourceType string,
	files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	return service.prepareMarkerProject(ctx, sourceType, files, onsMarkerProject)
}

func detectMarkerProject[File any, Profile any](
	files []fileset.SourceFile,
	paths map[int]string,
	diagnosticName string,
	toDetectorFile func(fileset.SourceFile) File,
	detect func(typedProjectIndex[File]) (Profile, error),
	marshal func(Profile) ([]byte, error),
) ([]byte, error) {
	index := typedProjectIndex[File]{
		localProjectPathOpener: make(localProjectPathOpener, len(files)),
		files:                  make([]File, 0, len(files)),
	}
	for _, file := range files {
		index.files = append(index.files, toDetectorFile(file))
		index.localProjectPathOpener[file.Path] = paths[file.SourceIndex]
	}
	profile, err := detect(index)
	if err != nil {
		return nil, fmt.Errorf("detect %s project: %w", diagnosticName, err)
	}
	contents, err := marshal(profile)
	if err != nil {
		return nil, fmt.Errorf("marshal %s project profile: %w", diagnosticName, err)
	}
	return contents, nil
}

func (service *Service) prepareMarkerProject(
	ctx context.Context,
	sourceType string,
	files []importSourceFile,
	definition markerProjectDefinition,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	return service.prepareProject(ctx, sourceType, files,
		func(files []importSourceFile) ([]preparedDisposition, preparedGroup, error) {
			return service.prepareMarkerProjectDirectory(files, definition)
		},
		func(ctx context.Context, file importSourceFile) (
			preparedDisposition, preparedGroup, preparedArchive, error,
		) {
			return service.prepareMarkerProjectArchive(ctx, file, definition)
		},
	)
}

func (service *Service) prepareMarkerProjectDirectory(
	files []importSourceFile,
	definition markerProjectDefinition,
) ([]preparedDisposition, preparedGroup, error) {
	input := directoryProjectInput(files)
	project, err := fileset.NormalizeProjectWithMarkers(input, definition.markers)
	if err != nil {
		return nil, preparedGroup{}, fmt.Errorf("normalize %s directory: %w", definition.name, err)
	}
	paths := make(map[int]string, len(project.Files))
	for _, file := range project.Files {
		paths[file.SourceIndex] = service.blobs.Path(files[file.SourceIndex].sha256)
	}
	snapshot, err := definition.detect(project.Files, paths)
	if err != nil {
		return nil, preparedGroup{}, fmt.Errorf("detect %s directory: %w", definition.name, err)
	}
	dispositions, sources := directoryProjectSources(files, project.Files)
	return dispositions, markerProjectGroup(
		sources, snapshot, definition, rpgMakerDirectoryTitle(files),
	), nil
}

func (service *Service) prepareMarkerProjectArchive(
	ctx context.Context,
	file importSourceFile,
	definition markerProjectDefinition,
) (preparedDisposition, preparedGroup, preparedArchive, error) {
	archiveFormat, reason := profileArchiveFormat(file.path)
	if reason != "" {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, ErrInvalid
	}
	entries, candidates, err := service.scanProjectArchive(ctx, file, archiveFormat)
	if err != nil {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, err
	}
	defer discardProjectArchiveCandidates(candidates)
	project, entryByOrdinal, err := normalizeArchiveProject(entries, definition)
	if err != nil {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, err
	}
	projectEntries := archiveProjectEntries(project.Files, entryByOrdinal)
	readMetadata, err := service.projectArchiveReadMetadata(ctx, file, projectEntries, candidates)
	if err != nil {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, err
	}
	paths, err := archiveProjectPaths(project.Files, readMetadata)
	if err != nil {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, err
	}
	snapshot, err := definition.detect(project.Files, paths)
	if err != nil {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, fmt.Errorf(
			"detect %s archive: %w", definition.name, err,
		)
	}
	materialized, err := projectArchiveMaterialization(projectEntries, candidates, readMetadata)
	if err != nil {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, err
	}
	sources := archiveProjectSources(file, project.Files)
	return sourceDisposition(file), markerProjectGroup(sources, snapshot, definition, file.path), preparedArchive{
		blobID: file.blobID, entries: entries, materialized: materialized,
	}, nil
}

func directoryProjectInput(files []importSourceFile) []fileset.SourceFile {
	input := make([]fileset.SourceFile, 0, len(files))
	for index, file := range files {
		input = append(input, fileset.SourceFile{Path: file.path, SizeBytes: file.size, SourceIndex: index})
	}
	return input
}

func directoryProjectSources(
	files []importSourceFile,
	projectFiles []fileset.SourceFile,
) ([]preparedDisposition, []preparedSource) {
	included := make(map[int]fileset.SourceFile, len(projectFiles))
	for _, file := range projectFiles {
		included[file.SourceIndex] = file
	}
	dispositions := make([]preparedDisposition, 0, len(files))
	sources := make([]preparedSource, 0, len(projectFiles))
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
	return dispositions, sources
}

func normalizeArchiveProject(
	entries []importing.ArchiveEntry,
	definition markerProjectDefinition,
) (fileset.Project, map[int]importing.ArchiveEntry, error) {
	input := make([]fileset.SourceFile, 0, len(entries))
	entryByOrdinal := make(map[int]importing.ArchiveEntry, len(entries))
	for _, entry := range entries {
		input = append(input, fileset.SourceFile{
			Path: entry.NormalizedPath, SizeBytes: entry.Size, SourceIndex: entry.Ordinal,
		})
		entryByOrdinal[entry.Ordinal] = entry
	}
	project, err := fileset.NormalizeProjectWithMarkers(input, definition.markers)
	if err != nil {
		return fileset.Project{}, nil, fmt.Errorf("normalize %s archive: %w", definition.name, err)
	}
	return project, entryByOrdinal, nil
}

func archiveProjectEntries(
	files []fileset.SourceFile,
	entryByOrdinal map[int]importing.ArchiveEntry,
) []importing.ArchiveEntry {
	entries := make([]importing.ArchiveEntry, 0, len(files))
	for _, file := range files {
		entries = append(entries, entryByOrdinal[file.SourceIndex])
	}
	return entries
}

func archiveProjectPaths(
	files []fileset.SourceFile,
	metadata map[int]blobstore.Metadata,
) (map[int]string, error) {
	paths := make(map[int]string, len(files))
	for _, file := range files {
		value, exists := metadata[file.SourceIndex]
		if !exists || value.Path == "" {
			return nil, importing.ErrArchiveUnsafe
		}
		paths[file.SourceIndex] = value.Path
	}
	return paths, nil
}

func archiveProjectSources(file importSourceFile, files []fileset.SourceFile) []preparedSource {
	sources := make([]preparedSource, 0, len(files))
	for _, projectFile := range files {
		ordinal := projectFile.SourceIndex
		sources = append(sources, preparedSource{
			file: file, role: "PROJECT_FILE", logicalName: projectFile.Path,
			archiveBlobID: file.blobID, archiveOrdinal: &ordinal,
		})
	}
	return sources
}

func markerProjectGroup(
	sources []preparedSource,
	snapshot []byte,
	definition markerProjectDefinition,
	titleSource string,
) preparedGroup {
	sortPreparedSources(sources)
	return preparedGroup{
		sources: sources, contentKind: definition.contentKind,
		validationStatus: "BLOCKED", compatibilityCode: definition.compatibilityCode,
		dependencySnapshot: string(snapshot), titleSource: titleSource, titleSourceExplicit: true,
	}
}
