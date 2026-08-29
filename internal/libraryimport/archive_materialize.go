package libraryimport

import (
	"context"
	"fmt"
	"io"

	"retrom/internal/blobstore"
	"retrom/internal/importing"
)

func (service *Service) materializeArchiveEntries(
	ctx context.Context,
	archivePath string,
	expected []importing.ArchiveEntry,
) (map[int]blobstore.Metadata, error) {
	materialized := make(map[int]blobstore.Metadata, len(expected))
	if len(expected) == 0 {
		return materialized, nil
	}
	if expected[0].ArchiveFormat != "SEVEN_Z" {
		for _, entry := range expected {
			metadata, err := service.materializeArchiveEntry(ctx, archivePath, entry)
			if err != nil {
				return nil, err
			}
			materialized[entry.Ordinal] = metadata
		}
		return materialized, nil
	}
	err := importing.ExtractSevenZipEntries(
		ctx, archivePath, expected,
		func(entry importing.ArchiveEntry, reader io.Reader) error {
			metadata, putErr := service.blobs.Put(reader)
			if putErr != nil {
				return fmt.Errorf("materialize archive entry: %w", putErr)
			}
			if !archiveMetadataMatches(entry, metadata) {
				return importing.ErrArchiveUnsafe
			}
			materialized[entry.Ordinal] = metadata
			return nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("materialize 7z archive: %w", err)
	}
	return materialized, nil
}

func archiveMetadataMatches(entry importing.ArchiveEntry, metadata blobstore.Metadata) bool {
	return metadata.Size == entry.Size && metadata.CRC32 == entry.CRC32 && metadata.MD5 == entry.MD5 &&
		metadata.SHA1 == entry.SHA1 && metadata.SHA256 == entry.SHA256
}
