package libraryimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"retrom/internal/cleanup"
	contentprofile "retrom/internal/content/profile"
	"retrom/internal/filestore"
	"retrom/internal/importing"
)

func (service *ImportPreparation) rpgMakerNestedArchiveFormat(
	file ImportFile,
) (importing.NestedArchiveFormat, error) {
	reader, err := os.Open(service.blobs.Path(file.FileRecord))
	if err != nil {
		return importing.NestedArchiveNone, fmt.Errorf("open RPG Maker project file: %w", err)
	}
	prefix, readErr := io.ReadAll(io.LimitReader(reader, 512))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		return importing.NestedArchiveNone, fmt.Errorf(
			"inspect RPG Maker project file: %w", errors.Join(readErr, closeErr),
		)
	}
	return importing.DetectNestedArchive(file.Path, prefix), nil
}

func (service *ImportPreparation) scanProjectArchive(
	ctx context.Context,
	file ImportFile,
	archiveFormat contentprofile.ArchiveFormat,
) ([]importing.ArchiveEntry, map[int]*filestore.Candidate, error) {
	return service.scanProjectArchivePath(ctx, service.blobs.Path(file.FileRecord), archiveFormat)
}

func (service *ImportPreparation) scanProjectArchivePath(
	ctx context.Context,
	archivePath string,
	archiveFormat contentprofile.ArchiveFormat,
) ([]importing.ArchiveEntry, map[int]*filestore.Candidate, error) {
	limits := importing.RPGMakerArchiveLimits()
	var entries []importing.ArchiveEntry
	candidates := make(map[int]*filestore.Candidate)
	var err error
	consumer := func(entry importing.ArchiveEntry, reader io.Reader) (importing.ArchiveContent, error) {
		candidate, stageErr := service.blobs.Stage(reader)
		if stageErr != nil {
			return importing.ArchiveContent{}, fmt.Errorf("stage project entry: %w", stageErr)
		}
		metadata := candidate.Metadata()
		candidates[entry.Ordinal] = candidate
		return importing.ArchiveContent{
			Size: metadata.Size, CRC32: metadata.CRC32, MD5: metadata.MD5,
			SHA1: metadata.SHA1, SHA256: metadata.SHA256,
		}, nil
	}
	switch archiveFormat {
	case contentprofile.ArchiveZIP:
		entries, err = importing.ScanZIPWithConsumer(
			ctx, archivePath, limits, consumer,
		)
	case contentprofile.ArchiveNWJSExecutable:
		entries, err = importing.ScanNWJSExecutableWithConsumer(
			ctx, archivePath, limits, consumer,
		)
	case contentprofile.ArchiveElectronASAR:
		entries, err = importing.ScanElectronASARZIPWithConsumer(
			ctx, archivePath, limits, consumer,
		)
	case contentprofile.ArchiveSevenZip:
		entries, err = importing.ScanSevenZip(ctx, archivePath, limits)
	default:
		err = importing.ErrArchiveUnsafe
	}
	if err != nil {
		discardProjectArchiveCandidates(candidates)
		return nil, nil, fmt.Errorf("libraryimport/RPG Maker archive: %w", err)
	}
	return entries, candidates, nil
}

func (service *ImportPreparation) projectArchiveReadMetadata(
	ctx context.Context,
	file ImportFile,
	entries []importing.ArchiveEntry,
	candidates map[int]*filestore.Candidate,
) (map[int]filestore.Metadata, error) {
	missing := make([]importing.ArchiveEntry, 0)
	result := make(map[int]filestore.Metadata, len(entries))
	for _, entry := range entries {
		if candidate, exists := candidates[entry.Ordinal]; exists {
			result[entry.Ordinal] = candidate.Metadata()
		} else {
			missing = append(missing, entry)
		}
	}
	if len(missing) == 0 {
		return result, nil
	}
	extracted, err := service.materializeArchiveEntries(ctx, service.blobs.Path(file.FileRecord), missing)
	if err != nil {
		return nil, err
	}
	for ordinal, metadata := range extracted {
		result[ordinal] = metadata
	}
	return result, nil
}

func projectArchiveMaterialization(
	entries []importing.ArchiveEntry,
	candidates map[int]*filestore.Candidate,
	readMetadata map[int]filestore.Metadata,
) (map[int]filestore.Metadata, error) {
	result := make(map[int]filestore.Metadata, len(entries))
	for _, entry := range entries {
		if candidate, exists := candidates[entry.Ordinal]; exists {
			metadata, err := candidate.Commit()
			if err != nil {
				return nil, fmt.Errorf("commit ZIP entry: %w", err)
			}
			result[entry.Ordinal] = metadata
			continue
		}
		metadata, exists := readMetadata[entry.Ordinal]
		if !exists {
			return nil, importing.ErrArchiveUnsafe
		}
		result[entry.Ordinal] = metadata
	}
	return result, nil
}

func discardProjectArchiveCandidates(candidates map[int]*filestore.Candidate) {
	for _, candidate := range candidates {
		cleanup.Error("discard project archive candidate", candidate.Discard())
	}
}
