package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"
	model "retrom/internal/model/libraryimport"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/engine/rpgmaker/fileset"
	"retrom/internal/capability/engine/scummvm"
	"retrom/internal/capability/format/importing"
)

func (service *ImportPreparation) PrepareScummVMProject(
	ctx context.Context,
	sourceType string,
	files []model.ImportFile,
) ([]model.PreparedDisposition, []model.PreparedGroup, []model.PreparedArchive, error) {
	if service.scummVMDetector == nil {
		return nil, nil, nil, scummvm.ErrToolFailed
	}
	return service.prepareProject(ctx, sourceType, files, func(
		files []model.ImportFile,
	) ([]model.PreparedDisposition, model.PreparedGroup, error) {
		return service.prepareScummVMDirectory(ctx, files)
	}, service.prepareScummVMArchive)
}

func (service *ImportPreparation) prepareScummVMDirectory(
	ctx context.Context,
	files []model.ImportFile,
) ([]model.PreparedDisposition, model.PreparedGroup, error) {
	project, err := fileset.NormalizeTree(directoryProjectInput(files))
	if err != nil {
		return nil, model.PreparedGroup{}, fmt.Errorf("normalize ScummVM directory: %w", err)
	}
	metadata := make(map[int]blobstore.Metadata, len(project.Files))
	for _, file := range project.Files {
		source := files[file.SourceIndex]
		metadata[file.SourceIndex] = blobstore.Metadata{
			Path: service.blobs.Path(source.SHA256), SHA256: source.SHA256, Size: source.Size,
		}
	}
	snapshot, err := service.detectScummVMTree(ctx, project.Files, metadata, nil)
	if err != nil {
		return nil, model.PreparedGroup{}, err
	}
	dispositions, sources := directoryProjectSources(files, project.Files)
	group, err := newScummVMGroup(sources, snapshot, RpgMakerDirectoryTitle(files))
	return dispositions, group, err
}

func (service *ImportPreparation) prepareScummVMArchive(
	ctx context.Context,
	file model.ImportFile,
) (model.PreparedDisposition, model.PreparedGroup, model.PreparedArchive, error) {
	format, reason := profileArchiveFormat(file.Path)
	if reason != "" || format != contentprofile.ArchiveZIP && format != contentprofile.ArchiveSevenZip {
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, model.ErrInvalid
	}
	entries, candidates, err := service.scanProjectArchive(ctx, file, format)
	if err != nil {
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, err
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
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, fmt.Errorf("normalize ScummVM archive: %w", err)
	}
	selected := archiveProjectEntries(project.Files, entryByOrdinal)
	metadata, err := service.projectArchiveReadMetadata(ctx, file, selected, candidates)
	if err != nil {
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, err
	}
	snapshot, err := service.detectScummVMTree(ctx, project.Files, metadata, &file)
	if err != nil {
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, err
	}
	materialized, err := projectArchiveMaterialization(selected, candidates, metadata)
	if err != nil {
		return model.PreparedDisposition{}, model.PreparedGroup{}, model.PreparedArchive{}, err
	}
	group, err := newScummVMGroup(archiveProjectSources(file, project.Files), snapshot, file.Path)
	archive := model.PreparedArchive{BlobID: file.BlobID, Entries: entries, Materialized: materialized}
	return sourceDisposition(file), group, archive, err
}

func newScummVMGroup(sources []model.PreparedSource, snapshot scummvm.Snapshot, title string) (model.PreparedGroup, error) {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return model.PreparedGroup{}, fmt.Errorf("encode ScummVM detection: %w", err)
	}
	status, code := snapshot.Status()
	sortPreparedSources(sources)
	return model.PreparedGroup{
		Sources: sources, ContentKind: scummvm.ContentKind, ValidationStatus: status,
		CompatibilityCode: code, DependencySnapshot: string(encoded), TitleSource: title, TitleSourceExplicit: true,
	}, nil
}
