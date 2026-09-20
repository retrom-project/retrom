package libraryimport

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/content/contentprofile"
	butterscotchdetector "retrom/internal/capability/engine/butterscotch/detector"
	onsdetector "retrom/internal/capability/engine/ons/detector"
	"retrom/internal/capability/engine/rpgmaker/fileset"
	tyranodetector "retrom/internal/capability/engine/tyranoscript/detector"
	"retrom/internal/capability/format/importing"
	model "retrom/internal/model/libraryimport"
)

type markerProjectDefinition struct {
	name              string
	contentKind       string
	compatibilityCode string
	markers           []string
	electronASAR      bool
}

var butterscotchMarkerProject = markerProjectDefinition{
	name: "Butterscotch", markers: butterscotchdetector.Markers(),
	contentKind:       string(contentprofile.ContentKindButterscotchProject),
	compatibilityCode: "BUTTERSCOTCH_RUNTIME_TRIAL_REQUIRED",
}

var onsMarkerProject = markerProjectDefinition{
	name: "ONS", markers: onsdetector.Markers(),
	contentKind:       string(contentprofile.ContentKindONSProject),
	compatibilityCode: "ONS_RUNTIME_TRIAL_REQUIRED",
}

var tyranoScriptMarkerProject = markerProjectDefinition{
	name: "TyranoScript", markers: tyranodetector.Markers(),
	contentKind:       string(contentprofile.ContentKindTyranoScriptProject),
	compatibilityCode: "TYRANOSCRIPT_RUNTIME_TRIAL_REQUIRED",
	electronASAR:      true,
}

func (service *ImportPreparation) PrepareButterscotchProject(
	ctx context.Context,
	sourceType string,
	files []model.ImportFile,
) ([]model.PreparedDisposition, []model.PreparedGroup, []model.PreparedArchive, error) {
	return service.prepareMarkerProject(ctx, sourceType, files, butterscotchMarkerProject)
}

func (service *ImportPreparation) PrepareONSProject(
	ctx context.Context,
	sourceType string,
	files []model.ImportFile,
) ([]model.PreparedDisposition, []model.PreparedGroup, []model.PreparedArchive, error) {
	return service.prepareMarkerProject(ctx, sourceType, files, onsMarkerProject)
}

func (service *ImportPreparation) PrepareTyranoScriptProject(
	ctx context.Context,
	sourceType string,
	files []model.ImportFile,
) ([]model.PreparedDisposition, []model.PreparedGroup, []model.PreparedArchive, error) {
	return service.prepareMarkerProject(ctx, sourceType, files, tyranoScriptMarkerProject)
}

func (service *ImportPreparation) prepareMarkerProject(
	ctx context.Context,
	sourceType string,
	files []model.ImportFile,
	definition markerProjectDefinition,
) ([]model.PreparedDisposition, []model.PreparedGroup, []model.PreparedArchive, error) {
	return service.prepareProject(ctx, sourceType, files,
		func(files []model.ImportFile) ([]model.PreparedDisposition, model.PreparedGroup, error) {
			return service.prepareMarkerProjectDirectory(ctx, files, definition)
		},
		func(
			ctx context.Context,
			file model.ImportFile,
		) (model.PreparedDisposition, model.PreparedGroup, model.PreparedArchive,
			error,
		) {
			return service.prepareMarkerProjectArchive(ctx, file, definition)
		},
	)
}

func (service *ImportPreparation) prepareMarkerProjectDirectory(
	ctx context.Context,
	files []model.ImportFile,
	definition markerProjectDefinition,
) ([]model.PreparedDisposition, model.PreparedGroup, error) {
	input := directoryProjectInput(files)
	project, err := fileset.NormalizeProjectWithMarkers(input, definition.markers)
	if err != nil {
		return nil, model.PreparedGroup{}, fmt.Errorf("normalize %s directory: %w", definition.name, err)
	}
	paths := make(map[int]string, len(project.Files))
	for _, file := range project.Files {
		paths[file.SourceIndex] = service.blobs.Path(files[file.SourceIndex].SHA256)
	}
	snapshot, err := service.detectMarkerProject(ctx, project.Files, paths, definition)
	if err != nil {
		return nil, model.PreparedGroup{}, fmt.Errorf("detect %s directory: %w", definition.name, err)
	}
	dispositions, sources := directoryProjectSources(files, project.Files)
	return dispositions, markerProjectGroup(
		sources, snapshot, definition, RpgMakerDirectoryTitle(files),
	), nil
}

func (service *ImportPreparation) prepareMarkerProjectArchive(
	ctx context.Context,
	file model.ImportFile,
	definition markerProjectDefinition,
) (model.PreparedDisposition, model.PreparedGroup, model.PreparedArchive, error) {
	format, err := service.resolveMarkerProjectArchiveFormat(ctx, file, definition)
	if err != nil {
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, err
	}
	entries, candidates, project, entryByOrdinal, err := service.scanMarkerProjectArchive(
		ctx, file, definition, format,
	)
	if err != nil {
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, err
	}
	defer func() { discardProjectArchiveCandidates(candidates) }()
	projectEntries := archiveProjectEntries(project.Files, entryByOrdinal)
	readMetadata, err := service.projectArchiveReadMetadata(ctx, file, projectEntries, candidates)
	if err != nil {
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, err
	}
	readPaths := make(map[int]string, len(readMetadata))
	for ordinal, value := range readMetadata {
		readPaths[ordinal] = value.Path
	}
	paths, err := ArchiveProjectPaths(project.Files, readPaths)
	if err != nil {
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, err
	}
	snapshot, err := service.detectMarkerProject(ctx, project.Files, paths, definition)
	if err != nil {
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, fmt.Errorf(
			"detect %s archive: %w", definition.name, err,
		)
	}
	materialized, err := projectArchiveMaterialization(projectEntries, candidates, readMetadata)
	if err != nil {
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, err
	}
	sources := archiveProjectSources(file, project.Files)
	return sourceDisposition(file), markerProjectGroup(sources, snapshot, definition, file.Path), model.PreparedArchive{
		BlobID: file.BlobID, Entries: entries, Materialized: materialized,
	}, nil
}

func (service *ImportPreparation) resolveMarkerProjectArchiveFormat(
	ctx context.Context,
	file model.ImportFile,
	definition markerProjectDefinition,
) (contentprofile.ArchiveFormat, error) {
	format, reason := ImportArchiveFormat(file.Path)
	if definition.electronASAR {
		format, reason = tyranoScriptArchiveFormat(file.Path)
	}
	if reason != "" {
		return "", model.ErrInvalid
	}
	if !definition.electronASAR || format != contentprofile.ArchiveZIP {
		return format, nil
	}
	detected, err := service.archiveInspector.DetectElectronASARZIP(
		ctx,
		service.blobs.Path(file.SHA256), importing.RPGMakerArchiveLimits(),
	)
	if err != nil {
		return "", fmt.Errorf("detect TyranoScript Electron archive: %w", err)
	}
	if detected {
		return contentprofile.ArchiveElectronASAR, nil
	}
	return format, nil
}

func (service *ImportPreparation) scanMarkerProjectArchive(
	ctx context.Context,
	file model.ImportFile,
	definition markerProjectDefinition,
	format contentprofile.ArchiveFormat,
) ([]importing.ArchiveEntry, map[int]*blobstore.Candidate, fileset.Project, map[int]importing.ArchiveEntry, error) {
	entries, candidates, err := service.scanProjectArchive(ctx, file, format)
	if err != nil {
		return nil, nil, fileset.Project{}, nil, err
	}
	project, entryByOrdinal, err := normalizeArchiveProject(entries, definition)
	if err != nil && definition.name == tyranoScriptMarkerProject.name &&
		format == contentprofile.ArchiveZIP && markerProjectNotFound(err) {
		wrappedEntries, wrappedCandidates, detected, wrappedErr := service.scanWrappedTyranoScriptNWJS(
			ctx, entries, candidates,
		)
		if wrappedErr != nil {
			discardProjectArchiveCandidates(candidates)
			return nil, nil, fileset.Project{}, nil, wrappedErr
		}
		if detected {
			discardProjectArchiveCandidates(candidates)
			entries, candidates = wrappedEntries, wrappedCandidates
			project, entryByOrdinal, err = normalizeArchiveProject(entries, definition)
		}
	}
	if err != nil {
		discardProjectArchiveCandidates(candidates)
		return nil, nil, fileset.Project{}, nil, err
	}
	return entries, candidates, project, entryByOrdinal, nil
}

func markerProjectNotFound(err error) bool {
	var projectError *fileset.ProjectError
	return errors.As(err, &projectError) && projectError.Code == fileset.CodeProjectNotFound
}

func (service *ImportPreparation) scanWrappedTyranoScriptNWJS(
	ctx context.Context,
	entries []importing.ArchiveEntry,
	candidates map[int]*blobstore.Candidate,
) ([]importing.ArchiveEntry, map[int]*blobstore.Candidate, bool, error) {
	selectedPath := ""
	for _, entry := range entries {
		if !strings.EqualFold(filepath.Ext(entry.NormalizedPath), ".exe") {
			continue
		}
		candidate, exists := candidates[entry.Ordinal]
		if !exists {
			return nil, nil, false, importing.ErrArchiveUnsafe
		}
		candidatePath := candidate.Path()
		if err := service.archiveInspector.ValidateNWJSExecutable(ctx, candidatePath); err != nil {
			if errors.Is(err, importing.ErrNWJSExecutableInvalid) {
				continue
			}
			return nil, nil, false, fmt.Errorf("validate wrapped TyranoScript NW.js executable: %w", err)
		}
		if selectedPath != "" {
			return nil, nil, false, fmt.Errorf(
				"select wrapped TyranoScript NW.js executable: %w", importing.ErrArchiveUnsafe,
			)
		}
		selectedPath = candidatePath
	}
	if selectedPath == "" {
		return nil, nil, false, nil
	}
	innerEntries, innerCandidates, err := service.scanProjectArchivePath(
		ctx, selectedPath, contentprofile.ArchiveNWJSExecutable,
	)
	if err != nil {
		return nil, nil, false, fmt.Errorf("scan wrapped TyranoScript NW.js executable: %w", err)
	}
	return innerEntries, innerCandidates, true, nil
}

func tyranoScriptArchiveFormat(filePath string) (contentprofile.ArchiveFormat, string) {
	if strings.EqualFold(filepath.Ext(filePath), ".exe") {
		return contentprofile.ArchiveNWJSExecutable, ""
	}
	return ImportArchiveFormat(filePath)
}

func directoryProjectInput(files []model.ImportFile) []fileset.SourceFile {
	input := make([]fileset.SourceFile, 0, len(files))
	for index, file := range files {
		input = append(input, fileset.SourceFile{Path: file.Path, SizeBytes: file.Size, SourceIndex: index})
	}
	return input
}

func directoryProjectSources(
	files []model.ImportFile,
	projectFiles []fileset.SourceFile,
) ([]model.PreparedDisposition, []model.PreparedSource) {
	included := make(map[int]fileset.SourceFile, len(projectFiles))
	for _, file := range projectFiles {
		included[file.SourceIndex] = file
	}
	dispositions := make([]model.PreparedDisposition, 0, len(files))
	sources := make([]model.PreparedSource, 0, len(projectFiles))
	for sourceIndex, source := range files {
		file, exists := included[sourceIndex]
		if !exists {
			dispositions = append(dispositions, model.PreparedDisposition{
				File: source, Disposition: "IGNORED", Reason: "IGNORED_SYSTEM_SIDECAR",
			})
			continue
		}
		dispositions = append(dispositions, sourceDisposition(source))
		sources = append(sources, model.PreparedSource{File: source, Role: "PROJECT_FILE", LogicalName: file.Path})
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

func ArchiveProjectPaths(
	files []fileset.SourceFile,
	metadata map[int]string,
) (map[int]string, error) {
	paths := make(map[int]string, len(files))
	for _, file := range files {
		value, exists := metadata[file.SourceIndex]
		if !exists || value == "" {
			return nil, importing.ErrArchiveUnsafe
		}
		paths[file.SourceIndex] = value
	}
	return paths, nil
}

func archiveProjectSources(file model.ImportFile, files []fileset.SourceFile) []model.PreparedSource {
	sources := make([]model.PreparedSource, 0, len(files))
	for _, projectFile := range files {
		ordinal := projectFile.SourceIndex
		sources = append(sources, model.PreparedSource{
			File: file, Role: "PROJECT_FILE", LogicalName: projectFile.Path,
			ArchiveBlobID: file.BlobID, ArchiveOrdinal: &ordinal,
		})
	}
	return sources
}

func markerProjectGroup(
	sources []model.PreparedSource,
	snapshot []byte,
	definition markerProjectDefinition,
	titleSource string,
) model.PreparedGroup {
	sortPreparedSources(sources)
	return model.PreparedGroup{
		Sources: sources, ContentKind: definition.contentKind,
		ValidationStatus: "BLOCKED", CompatibilityCode: definition.compatibilityCode,
		DependencySnapshot: string(snapshot), TitleSource: titleSource, TitleSourceExplicit: true,
	}
}
