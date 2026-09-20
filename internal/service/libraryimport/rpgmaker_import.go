package libraryimport

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/engine/rpgmaker/detector"
	"retrom/internal/capability/engine/rpgmaker/fileset"
	"retrom/internal/capability/format/importing"
	model "retrom/internal/model/libraryimport"
)

func (service *ImportPreparation) PrepareRPGMakerProject(
	ctx context.Context,
	sourceType string,
	files []model.ImportFile,
	coreID string,
) ([]model.PreparedDisposition, []model.PreparedGroup, []model.PreparedArchive, error) {
	if service.blobs == nil || service.rpgMakerDetector == nil {
		return nil, nil, nil, model.ErrInvalid
	}
	if sourceType == "DIRECTORY" {
		dispositions, group, err := service.prepareRPGMakerDirectory(ctx, files, coreID)
		if err != nil {
			return nil, nil, nil, err
		}
		return dispositions, []model.PreparedGroup{group}, nil, nil
	}
	if sourceType != "FILES" || len(files) != 1 {
		return nil, nil, nil, model.ErrInvalid
	}
	disposition, group, archive, err := service.prepareRPGMakerArchive(ctx, files[0], coreID)
	if err != nil {
		return nil, nil, nil, err
	}
	return []model.PreparedDisposition{disposition}, []model.PreparedGroup{group}, []model.PreparedArchive{archive}, nil
}

func (service *ImportPreparation) prepareRPGMakerDirectory(
	ctx context.Context,
	files []model.ImportFile,
	coreID string,
) ([]model.PreparedDisposition, model.PreparedGroup, error) {
	input := make([]fileset.SourceFile, 0, len(files))
	for index, file := range files {
		nestedFormat, err := service.rpgMakerNestedArchiveFormat(file)
		if err != nil {
			return nil, model.PreparedGroup{}, err
		}
		input = append(input, fileset.SourceFile{
			Path: file.Path, SizeBytes: file.Size, SourceIndex: index,
			NestedArchiveFormat: nestedFormat,
		})
	}
	project, err := fileset.NormalizeProject(input)
	if err != nil {
		return nil, model.PreparedGroup{}, fmt.Errorf("normalize RPG Maker directory: %w", err)
	}
	probes := make([]model.RPGMakerProbeFile, 0, len(project.Files))
	for _, file := range project.Files {
		source := files[file.SourceIndex]
		probes = append(probes, model.RPGMakerProbeFile{
			File:       detector.File{Path: file.Path, Size: file.SizeBytes},
			SourcePath: service.blobs.Path(source.SHA256),
		})
	}
	profile, err := service.rpgMakerDetector.DetectPrepared(ctx, coreID, probes)
	if err != nil {
		return nil, model.PreparedGroup{}, fmt.Errorf("detect RPG Maker directory: %w", err)
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
	dispositions := make([]model.PreparedDisposition, 0, len(files))
	sources := make([]model.PreparedSource, 0, len(projectFiles))
	for sourceIndex, source := range files {
		file, exists := included[sourceIndex]
		if !exists {
			reason := "IGNORED_SYSTEM_SIDECAR"
			if _, isSessionState := sessionStateIndices[sourceIndex]; isSessionState {
				reason = "RPG_SESSION_STATE_EXCLUDED"
			}
			dispositions = append(dispositions, model.PreparedDisposition{
				File: source, Disposition: "IGNORED", Reason: reason,
			})
			continue
		}
		dispositions = append(dispositions, sourceDisposition(source))
		sources = append(sources, model.PreparedSource{File: source, Role: "PROJECT_FILE", LogicalName: file.Path})
	}
	sortPreparedSources(sources)
	removed := append([]string(nil), project.RemovedNoise...)
	removed = append(removed, sessionState...)
	return dispositions, newRPGMakerGroup(
		sources, profile, project.Root, removed, RpgMakerDirectoryTitle(files),
	), nil
}

func (service *ImportPreparation) prepareRPGMakerArchive(
	ctx context.Context,
	file model.ImportFile,
	coreID string,
) (model.PreparedDisposition, model.PreparedGroup, model.PreparedArchive, error) {
	archiveFormat, reason := ImportArchiveFormat(file.Path)
	if reason != "" {
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, model.ErrInvalid
	}
	entries, candidates, err := service.scanProjectArchive(ctx, file, archiveFormat)
	if err != nil {
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, err
	}
	defer discardProjectArchiveCandidates(candidates)
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
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, fmt.Errorf(
			"normalize RPG Maker archive: %w", err,
		)
	}
	projectEntries := make([]importing.ArchiveEntry, 0, len(project.Files))
	for _, projectFile := range project.Files {
		projectEntries = append(projectEntries, entryByOrdinal[projectFile.SourceIndex])
	}
	readMetadata, err := service.projectArchiveReadMetadata(ctx, file, projectEntries, candidates)
	if err != nil {
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, err
	}
	probes := make([]model.RPGMakerProbeFile, 0, len(project.Files))
	for _, projectFile := range project.Files {
		entry := entryByOrdinal[projectFile.SourceIndex]
		probes = append(probes, model.RPGMakerProbeFile{
			File:       detector.File{Path: projectFile.Path, Size: projectFile.SizeBytes},
			SourcePath: readMetadata[entry.Ordinal].Path,
		})
	}
	profile, err := service.rpgMakerDetector.DetectPrepared(ctx, coreID, probes)
	if err != nil {
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, fmt.Errorf(
			"detect RPG Maker archive: %w", err,
		)
	}
	projectFiles, sessionState := fileset.ExcludeSessionState(profile.ExpectedGeneration, project.Files)
	sources := make([]model.PreparedSource, 0, len(projectFiles))
	for _, projectFile := range projectFiles {
		ordinal := projectFile.SourceIndex
		sources = append(sources, model.PreparedSource{
			File: file, Role: "PROJECT_FILE", LogicalName: projectFile.Path,
			ArchiveBlobID: file.BlobID, ArchiveOrdinal: &ordinal,
		})
	}
	selectedEntries := projectEntriesForFiles(projectFiles, entryByOrdinal)
	materialized, err := projectArchiveMaterialization(selectedEntries, candidates, readMetadata)
	if err != nil {
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, err
	}
	sortPreparedSources(sources)
	removed := append([]string(nil), project.RemovedNoise...)
	removed = append(removed, sessionState...)
	return sourceDisposition(file), newRPGMakerGroup(
			sources, profile, project.Root, removed, file.Path,
		), model.PreparedArchive{
			BlobID: file.BlobID, Entries: entries, Materialized: materialized,
		}, nil
}

func newRPGMakerGroup(
	sources []model.PreparedSource,
	profile detector.Profile,
	root string,
	removed []string,
	titleSource string,
) model.PreparedGroup {
	profileCopy := profile
	sort.Strings(removed)
	return model.PreparedGroup{
		Sources: sources, ContentKind: string(contentprofile.ContentKindRPGMakerProject),
		TitleSource: titleSource, TitleSourceExplicit: true, RPGProfile: &profileCopy,
		RPGProjectRoot: root, RPGRemovedFiles: removed,
	}
}

func RpgMakerDirectoryTitle(files []model.ImportFile) string {
	if len(files) == 0 {
		return ""
	}
	root, _, hasChild := strings.Cut(filepath.ToSlash(files[0].Path), "/")
	if !hasChild || root == "" {
		return ""
	}
	for _, file := range files[1:] {
		candidate, _, candidateHasChild := strings.Cut(filepath.ToSlash(file.Path), "/")
		if !candidateHasChild || candidate != root {
			return ""
		}
	}
	return root
}

func projectEntriesForFiles(
	files []fileset.SourceFile,
	entryByOrdinal map[int]importing.ArchiveEntry,
) []importing.ArchiveEntry {
	entries := make([]importing.ArchiveEntry, 0, len(files))
	for _, file := range files {
		entries = append(entries, entryByOrdinal[file.SourceIndex])
	}
	return entries
}

func sortPreparedSources(sources []model.PreparedSource) {
	sort.Slice(sources, func(left, right int) bool {
		return sources[left].LogicalName < sources[right].LogicalName
	})
}
