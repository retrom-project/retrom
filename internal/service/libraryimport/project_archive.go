package libraryimport

import (
	"context"
	"fmt"
	"io"
	"sort"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/format/importing"
	"retrom/internal/model/diagnostics"
	model "retrom/internal/model/libraryimport"
)

func (service *ImportPreparation) scanProjectArchivePath(
	ctx context.Context,
	archivePath string,
	archiveFormat contentprofile.ArchiveFormat,
) ([]importing.ArchiveEntry, map[int]*blobstore.Candidate, error) {
	candidates := make(map[int]*blobstore.Candidate)
	var entries []importing.ArchiveEntry
	var err error
	switch archiveFormat {
	case contentprofile.ArchiveSevenZip:
		entries, err = importing.ScanSevenZip(ctx, archivePath, importing.RPGMakerArchiveLimits())
		if err != nil {
			err = fmt.Errorf("libraryimport/RPG Maker archive: %w", err)
		}
	case contentprofile.ArchiveZIP, contentprofile.ArchiveNWJSExecutable, contentprofile.ArchiveElectronASAR:
		entries, err = service.stageProjectArchive(ctx, archivePath, archiveFormat, candidates)
	default:
		err = fmt.Errorf("libraryimport/RPG Maker archive: %w", importing.ErrArchiveUnsafe)
	}
	if err != nil {
		discardProjectArchiveCandidates(candidates)
		return nil, nil, err
	}
	return entries, candidates, nil
}

func (service *ImportPreparation) stageProjectArchive(
	ctx context.Context,
	path string,
	format contentprofile.ArchiveFormat,
	candidates map[int]*blobstore.Candidate,
) ([]importing.ArchiveEntry, error) {
	if service.projectArchives == nil || service.diagnostics == nil {
		return nil, fmt.Errorf("libraryimport/RPG Maker archive: %w", model.ErrInvalid)
	}
	reader, err := service.projectArchives.OpenProject(ctx, path, format, importing.RPGMakerArchiveLimits())
	if err != nil {
		return nil, fmt.Errorf("libraryimport/RPG Maker archive: %w", err)
	}
	if reader == nil {
		return nil, fmt.Errorf("libraryimport/RPG Maker archive: %w", importing.ErrArchiveUnsafe)
	}
	closed := false
	defer func() {
		if !closed {
			service.reportProjectArchiveClose(ctx, reader.Close())
		}
	}()
	entries := make([]importing.ArchiveEntry, 0)
	for {
		header, nextErr := reader.Next()
		if nextErr == io.EOF {
			sort.Slice(entries, func(left, right int) bool { return entries[left].Ordinal < entries[right].Ordinal })
			return entries, nil
		}
		if nextErr != nil {
			return nil, fmt.Errorf("libraryimport/RPG Maker archive: %w", nextErr)
		}
		candidate, stageErr := service.blobs.Stage(reader)
		if stageErr != nil {
			closed = true
			closeErr := reader.Close()
			failure := importing.ProjectArchiveStageError(header, fmt.Errorf("stage project entry: %w", stageErr), closeErr)
			return nil, fmt.Errorf("libraryimport/RPG Maker archive: %w", failure)
		}
		candidates[header.Entry.Ordinal] = candidate
		metadata := candidate.Metadata()
		entry, completeErr := reader.Complete(importing.ArchiveContent{
			Size: metadata.Size, CRC32: metadata.CRC32, MD5: metadata.MD5,
			SHA1: metadata.SHA1, SHA256: metadata.SHA256,
		})
		if completeErr != nil {
			return nil, fmt.Errorf("libraryimport/RPG Maker archive: %w", completeErr)
		}
		entries = append(entries, entry)
	}
}

func (service *ImportPreparation) reportProjectArchiveClose(ctx context.Context, err error) {
	if err != nil {
		service.diagnostics.Report(ctx, diagnostics.CleanupFailure("close", "", fmt.Sprintf("%T", err)))
	}
}
