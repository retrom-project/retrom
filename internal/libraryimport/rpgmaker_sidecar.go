package libraryimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/contentprofile"
	"retrom/internal/importing"
)

func (service *Service) rpgMakerNestedArchiveFormat(
	file importSourceFile,
) (importing.NestedArchiveFormat, error) {
	reader, err := os.Open(service.blobs.Path(file.sha256))
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
	return importing.DetectNestedArchive(file.path, prefix), nil
}

func (service *Service) scanProjectArchive(
	ctx context.Context,
	file importSourceFile,
	archiveFormat contentprofile.ArchiveFormat,
) ([]importing.ArchiveEntry, map[int]*blobstore.Candidate, error) {
	archivePath := service.blobs.Path(file.sha256)
	limits := importing.RPGMakerArchiveLimits()
	var entries []importing.ArchiveEntry
	candidates := make(map[int]*blobstore.Candidate)
	var err error
	if archiveFormat == contentprofile.ArchiveZIP {
		entries, err = importing.ScanZIPWithConsumer(
			ctx, archivePath, limits,
			func(entry importing.ArchiveEntry, reader io.Reader) (importing.ArchiveContent, error) {
				candidate, stageErr := service.blobs.Stage(reader)
				if stageErr != nil {
					return importing.ArchiveContent{}, fmt.Errorf("stage ZIP entry: %w", stageErr)
				}
				metadata := candidate.Metadata()
				candidates[entry.Ordinal] = candidate
				return importing.ArchiveContent{
					Size: metadata.Size, CRC32: metadata.CRC32, MD5: metadata.MD5,
					SHA1: metadata.SHA1, SHA256: metadata.SHA256,
				}, nil
			},
		)
	} else {
		entries, err = importing.ScanSevenZip(ctx, archivePath, limits)
	}
	if err != nil {
		discardProjectArchiveCandidates(candidates)
		return nil, nil, fmt.Errorf("libraryimport/RPG Maker archive: %w", err)
	}
	return entries, candidates, nil
}

func (service *Service) projectArchiveReadMetadata(
	ctx context.Context,
	file importSourceFile,
	entries []importing.ArchiveEntry,
	candidates map[int]*blobstore.Candidate,
) (map[int]blobstore.Metadata, error) {
	missing := make([]importing.ArchiveEntry, 0)
	result := make(map[int]blobstore.Metadata, len(entries))
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
	extracted, err := service.materializeArchiveEntries(ctx, service.blobs.Path(file.sha256), missing)
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
	candidates map[int]*blobstore.Candidate,
	readMetadata map[int]blobstore.Metadata,
) (map[int]blobstore.Metadata, error) {
	result := make(map[int]blobstore.Metadata, len(entries))
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

func discardProjectArchiveCandidates(candidates map[int]*blobstore.Candidate) {
	for _, candidate := range candidates {
		cleanup.Error("discard project archive candidate", candidate.Discard())
	}
}
