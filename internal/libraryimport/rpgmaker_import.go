package libraryimport

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"retrom/internal/contentprofile"
	"retrom/internal/importing"
	"retrom/internal/rpgmaker/detector"
	"retrom/internal/rpgmaker/fileset"
)

type rpgProjectIndex struct {
	files []detector.File
	paths map[string]string
}

func (index rpgProjectIndex) Files() []detector.File {
	return append([]detector.File(nil), index.files...)
}

func (index rpgProjectIndex) Open(logicalPath string) (io.ReadCloser, error) {
	localPath, exists := index.paths[logicalPath]
	if !exists {
		return nil, os.ErrNotExist
	}
	reader, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("open RPG Maker project file: %w", err)
	}
	return reader, nil
}

func (service *Service) prepareRPGMakerProject(
	ctx context.Context,
	sourceType string,
	files []importSourceFile,
	coreID string,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	if service.blobs == nil {
		return nil, nil, nil, ErrInvalid
	}
	if sourceType == "DIRECTORY" {
		dispositions, group, err := service.prepareRPGMakerDirectory(files, coreID)
		if err != nil {
			return nil, nil, nil, err
		}
		return dispositions, []preparedGroup{group}, nil, nil
	}
	if sourceType != "FILES" || len(files) != 1 {
		return nil, nil, nil, ErrInvalid
	}
	disposition, group, archive, err := service.prepareRPGMakerArchive(ctx, files[0], coreID)
	if err != nil {
		return nil, nil, nil, err
	}
	return []preparedDisposition{disposition}, []preparedGroup{group}, []preparedArchive{archive}, nil
}

func (service *Service) prepareRPGMakerDirectory(
	files []importSourceFile,
	coreID string,
) ([]preparedDisposition, preparedGroup, error) {
	input := make([]fileset.SourceFile, 0, len(files))
	for index, file := range files {
		nestedFormat, err := service.rpgMakerNestedArchiveFormat(file)
		if err != nil {
			return nil, preparedGroup{}, err
		}
		input = append(input, fileset.SourceFile{
			Path: file.path, SizeBytes: file.size, SourceIndex: index,
			NestedArchiveFormat: nestedFormat,
		})
	}
	project, err := fileset.NormalizeProject(input)
	if err != nil {
		return nil, preparedGroup{}, fmt.Errorf("normalize RPG Maker directory: %w", err)
	}
	index := rpgProjectIndex{paths: make(map[string]string, len(project.Files))}
	for _, file := range project.Files {
		source := files[file.SourceIndex]
		index.files = append(index.files, detector.File{Path: file.Path, Size: file.SizeBytes})
		index.paths[file.Path] = service.blobs.Path(source.sha256)
	}
	profile, err := detector.Detect(coreID, index)
	if err != nil {
		return nil, preparedGroup{}, fmt.Errorf("detect RPG Maker directory: %w", err)
	}
	projectFiles, sessionState := fileset.ExcludeSessionState(profile.ExpectedGeneration, project.Files)
	included := make(map[int]fileset.SourceFile, len(projectFiles))
	for _, file := range projectFiles {
		included[file.SourceIndex] = file
	}
	sessionStateIndices := make(map[int]struct{}, len(project.Files)-len(projectFiles))
	for _, file := range project.Files {
		if _, exists := included[file.SourceIndex]; !exists {
			sessionStateIndices[file.SourceIndex] = struct{}{}
		}
	}
	dispositions := make([]preparedDisposition, 0, len(files))
	sources := make([]preparedSource, 0, len(projectFiles))
	for sourceIndex, source := range files {
		file, exists := included[sourceIndex]
		if !exists {
			reason := "IGNORED_SYSTEM_SIDECAR"
			if _, isSessionState := sessionStateIndices[sourceIndex]; isSessionState {
				reason = "RPG_SESSION_STATE_EXCLUDED"
			}
			dispositions = append(dispositions, preparedDisposition{
				file: source, disposition: "IGNORED", reason: reason,
			})
			continue
		}
		dispositions = append(dispositions, sourceDisposition(source))
		sources = append(sources, preparedSource{file: source, role: "PROJECT_FILE", logicalName: file.Path})
	}
	sortPreparedSources(sources)
	removed := append([]string(nil), project.RemovedNoise...)
	removed = append(removed, sessionState...)
	return dispositions, newRPGMakerGroup(
		sources, profile, project.Root, removed, rpgMakerDirectoryTitle(files),
	), nil
}

func (service *Service) prepareRPGMakerArchive(
	ctx context.Context,
	file importSourceFile,
	coreID string,
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
			NestedArchiveFormat: entry.NestedArchive,
		})
		entryByOrdinal[entry.Ordinal] = entry
	}
	project, err := fileset.NormalizeProject(input)
	if err != nil {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, fmt.Errorf(
			"normalize RPG Maker archive: %w", err,
		)
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
	index := rpgProjectIndex{paths: make(map[string]string, len(project.Files))}
	for _, projectFile := range project.Files {
		entry := entryByOrdinal[projectFile.SourceIndex]
		index.files = append(index.files, detector.File{Path: projectFile.Path, Size: projectFile.SizeBytes})
		index.paths[projectFile.Path] = materialized[entry.Ordinal].Path
	}
	profile, err := detector.Detect(coreID, index)
	if err != nil {
		return preparedDisposition{}, preparedGroup{}, preparedArchive{}, fmt.Errorf(
			"detect RPG Maker archive: %w", err,
		)
	}
	projectFiles, sessionState := fileset.ExcludeSessionState(profile.ExpectedGeneration, project.Files)
	sources := make([]preparedSource, 0, len(projectFiles))
	keep := make(map[int]struct{}, len(projectFiles))
	for _, projectFile := range projectFiles {
		ordinal := projectFile.SourceIndex
		keep[ordinal] = struct{}{}
		sources = append(sources, preparedSource{
			file: file, role: "PROJECT_FILE", logicalName: projectFile.Path,
			archiveBlobID: file.blobID, archiveOrdinal: &ordinal,
		})
	}
	for ordinal := range materialized {
		if _, exists := keep[ordinal]; !exists {
			delete(materialized, ordinal)
		}
	}
	sortPreparedSources(sources)
	removed := append([]string(nil), project.RemovedNoise...)
	removed = append(removed, sessionState...)
	return sourceDisposition(file), newRPGMakerGroup(
			sources, profile, project.Root, removed, file.path,
		), preparedArchive{
			blobID: file.blobID, entries: entries, materialized: materialized,
		}, nil
}

func newRPGMakerGroup(
	sources []preparedSource,
	profile detector.Profile,
	root string,
	removed []string,
	titleSource string,
) preparedGroup {
	profileCopy := profile
	sort.Strings(removed)
	return preparedGroup{
		sources: sources, contentKind: string(contentprofile.ContentKindRPGMakerProject),
		validationStatus: "BLOCKED", compatibilityCode: "RPG_RUNTIME_VALIDATION_REQUIRED",
		dependencySnapshot: `{"bindings":[],"schemaVersion":1}`,
		titleSource:        titleSource, titleSourceExplicit: true, rpgProfile: &profileCopy,
		rpgProjectRoot: root, rpgRemovedFiles: removed,
	}
}

func rpgMakerDirectoryTitle(files []importSourceFile) string {
	if len(files) == 0 {
		return ""
	}
	root, _, hasChild := strings.Cut(filepath.ToSlash(files[0].path), "/")
	if !hasChild || root == "" {
		return ""
	}
	for _, file := range files[1:] {
		candidate, _, candidateHasChild := strings.Cut(filepath.ToSlash(file.path), "/")
		if !candidateHasChild || candidate != root {
			return ""
		}
	}
	return root
}

func sortPreparedSources(sources []preparedSource) {
	sort.Slice(sources, func(left, right int) bool {
		return sources[left].logicalName < sources[right].logicalName
	})
}
