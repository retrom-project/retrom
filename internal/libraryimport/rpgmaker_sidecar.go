package libraryimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

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

func (service *Service) scanRPGMakerArchive(
	ctx context.Context,
	file importSourceFile,
	archiveFormat contentprofile.ArchiveFormat,
) ([]importing.ArchiveEntry, error) {
	archivePath := service.blobs.Path(file.sha256)
	limits := importing.RPGMakerArchiveLimits()
	var entries []importing.ArchiveEntry
	var err error
	if archiveFormat == contentprofile.ArchiveZIP {
		entries, err = importing.ScanZIP(ctx, archivePath, limits)
	} else {
		entries, err = importing.ScanSevenZip(ctx, archivePath, limits)
	}
	if err != nil {
		return nil, fmt.Errorf("libraryimport/RPG Maker archive: %w", err)
	}
	return entries, nil
}
