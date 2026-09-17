package libraryimport

import (
	"context"
	"fmt"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/engine/kirikiri/detector"
	"retrom/internal/capability/engine/rpgmaker/fileset"
	"retrom/internal/capability/format/importing"
)

type kirikiriProjectIndex struct{ files []detector.File }

func (index kirikiriProjectIndex) Files() []detector.File {
	return append([]detector.File(nil), index.files...)
}

func (service *ImportPreparation) PrepareKiriKiriProject(
	ctx context.Context,
	sourceType string,
	files []model.ImportFile,
) ([]model.PreparedDisposition, []model.PreparedGroup, []model.PreparedArchive, error) {
	return service.prepareProject(
		ctx, sourceType, files, service.prepareKiriKiriDirectory, service.prepareKiriKiriArchive,
	)
}

func (service *ImportPreparation) prepareKiriKiriDirectory(
	files []model.ImportFile,
) ([]model.PreparedDisposition, model.PreparedGroup, error) {
	input := make([]fileset.SourceFile, 0, len(files))
	for index, file := range files {
		input = append(input, fileset.SourceFile{Path: file.Path, SizeBytes: file.Size, SourceIndex: index})
	}
	project, err := fileset.NormalizeProjectWithMarkers(input, detector.Markers())
	if err != nil {
		return nil, model.PreparedGroup{}, fmt.Errorf("normalize KiriKiri directory: %w", err)
	}
	index := kirikiriProjectIndex{files: make([]detector.File, 0, len(project.Files))}
	for _, file := range project.Files {
		index.files = append(index.files, detector.File{Path: file.Path, Size: file.SizeBytes})
	}
	profile, err := detector.Detect(index)
	if err != nil {
		return nil, model.PreparedGroup{}, fmt.Errorf("detect KiriKiri directory: %w", err)
	}
	dispositions := make([]model.PreparedDisposition, 0, len(files))
	sources := make([]model.PreparedSource, 0, len(project.Files))
	included := make(map[int]fileset.SourceFile, len(project.Files))
	for _, file := range project.Files {
		included[file.SourceIndex] = file
	}
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
	return dispositions, newKiriKiriGroup(sources, profile, RpgMakerDirectoryTitle(files)), nil
}

func (service *ImportPreparation) prepareKiriKiriArchive(
	ctx context.Context,
	file model.ImportFile,
) (model.PreparedDisposition, model.PreparedGroup, model.PreparedArchive, error) {
	archiveFormat, reason := profileArchiveFormat(file.Path)
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
		})
		entryByOrdinal[entry.Ordinal] = entry
	}
	project, err := fileset.NormalizeProjectWithMarkers(input, detector.Markers())
	if err != nil {
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, fmt.Errorf(
			"normalize KiriKiri archive: %w",
			err,
		)
	}
	projectEntries := make([]importing.ArchiveEntry, 0, len(project.Files))
	index := kirikiriProjectIndex{files: make([]detector.File, 0, len(project.Files))}
	for _, projectFile := range project.Files {
		projectEntries = append(projectEntries, entryByOrdinal[projectFile.SourceIndex])
		index.files = append(index.files, detector.File{Path: projectFile.Path, Size: projectFile.SizeBytes})
	}
	readMetadata, err := service.projectArchiveReadMetadata(ctx, file, projectEntries, candidates)
	if err != nil {
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, err
	}
	profile, err := detector.Detect(index)
	if err != nil {
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, fmt.Errorf(
			"detect KiriKiri archive: %w",
			err,
		)
	}
	materialized, err := projectArchiveMaterialization(projectEntries, candidates, readMetadata)
	if err != nil {
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, err
	}
	sources := make([]model.PreparedSource, 0, len(project.Files))
	for _, projectFile := range project.Files {
		ordinal := projectFile.SourceIndex
		sources = append(sources, model.PreparedSource{
			File: file, Role: "PROJECT_FILE", LogicalName: projectFile.Path,
			ArchiveBlobID: file.BlobID, ArchiveOrdinal: &ordinal,
		})
	}
	return sourceDisposition(file), newKiriKiriGroup(sources, profile, file.Path), model.PreparedArchive{
		BlobID: file.BlobID, Entries: entries, Materialized: materialized,
	}, nil
}

func newKiriKiriGroup(
	sources []model.PreparedSource,
	profile detector.Profile,
	titleSource string,
) model.PreparedGroup {
	sortPreparedSources(sources)
	profileJSON, _ := detector.MarshalSnapshot(profile)
	return model.PreparedGroup{
		Sources: sources, ContentKind: string(contentprofile.ContentKindKiriKiriProject),
		ValidationStatus: "BLOCKED", CompatibilityCode: "KIRIKIRI_RUNTIME_TRIAL_REQUIRED",
		DependencySnapshot: string(profileJSON), TitleSource: titleSource, TitleSourceExplicit: true,
	}
}
