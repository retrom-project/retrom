package libraryimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	blobmodel "retrom/internal/model/blob"
	model "retrom/internal/model/libraryimport"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/format/importing"
	"retrom/internal/foundation/cleanup"
)

func (service *ImportPreparation) rpgMakerNestedArchiveFormat(
	file model.ImportFile,
) (importing.NestedArchiveFormat, error) {
	reader, err := os.Open(service.blobs.Path(file.SHA256))
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
	file model.ImportFile,
	archiveFormat contentprofile.ArchiveFormat,
) ([]importing.ArchiveEntry, map[int]*blobstore.Candidate, error) {
	return service.scanProjectArchivePath(ctx, service.blobs.Path(file.SHA256), archiveFormat)
}

type projectArchiveInput struct {
	blobmodel.PreparedBlob
	Path string
}

func (service *ImportPreparation) projectArchiveReadMetadata(
	ctx context.Context,
	file model.ImportFile,
	entries []importing.ArchiveEntry,
	candidates map[int]*blobstore.Candidate,
) (map[int]projectArchiveInput, error) {
	missing := make([]importing.ArchiveEntry, 0)
	result := make(map[int]projectArchiveInput, len(entries))
	for _, entry := range entries {
		if candidate, exists := candidates[entry.Ordinal]; exists {
			result[entry.Ordinal] = projectArchiveInput{PreparedBlob: candidate.Metadata(), Path: candidate.Path()}
		} else {
			missing = append(missing, entry)
		}
	}
	if len(missing) == 0 {
		return result, nil
	}
	extracted, err := service.materializeArchiveEntries(ctx, service.blobs.Path(file.SHA256), missing)
	if err != nil {
		return nil, err
	}
	for ordinal, metadata := range extracted {
		result[ordinal] = projectArchiveInput{PreparedBlob: metadata, Path: service.blobs.Path(metadata.SHA256)}
	}
	return result, nil
}

func projectArchiveMaterialization(
	entries []importing.ArchiveEntry,
	candidates map[int]*blobstore.Candidate,
	readMetadata map[int]projectArchiveInput,
) (map[int]blobmodel.PreparedBlob, error) {
	result := make(map[int]blobmodel.PreparedBlob, len(entries))
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
		result[entry.Ordinal] = metadata.PreparedBlob
	}
	return result, nil
}

func discardProjectArchiveCandidates(candidates map[int]*blobstore.Candidate) {
	for _, candidate := range candidates {
		cleanup.Error("discard project archive candidate", candidate.Discard())
	}
}
