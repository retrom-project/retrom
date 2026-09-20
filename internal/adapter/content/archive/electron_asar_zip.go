package archive

import (
	"archive/zip"
	"context"
	"fmt"
	"os"

	"retrom/internal/capability/format/importing"
)

type electronZIPArchive struct {
	archive zipArchive
	layout  importing.ElectronZIPLayout
}

func (factory *Factory) DetectElectronASARZIP(
	ctx context.Context, path string, limits importing.ArchiveLimits,
) (bool, error) {
	archive, detected, err := factory.loadElectronZIP(ctx, path, limits)
	if archive != nil {
		closeZIP(ctx, factory.reporter, &archive.archive)
	}
	return detected, err
}

func (factory *Factory) loadElectronZIP(
	ctx context.Context, path string, limits importing.ArchiveLimits,
) (*electronZIPArchive, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, fmt.Errorf("open Electron ZIP: %w", err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		closeResource(ctx, factory.reporter, file)
		return nil, false, importing.ErrArchiveUnsafe
	}
	reader, err := zip.NewReader(file, info.Size())
	if err != nil {
		closeResource(ctx, factory.reporter, file)
		return nil, false, fmt.Errorf("%w: invalid Electron ZIP", importing.ErrArchiveUnsafe)
	}
	members, err := validateZIPDirectory(ctx, reader, limits, false)
	if err != nil {
		closeResource(ctx, factory.reporter, file)
		return nil, false, err
	}
	layout, detected, err := importing.LocateElectronZIPLayout(members)
	if err != nil {
		closeResource(ctx, factory.reporter, file)
		return nil, false, importing.ErrElectronASARInvalid
	}
	return &electronZIPArchive{
		archive: zipArchive{file: file, reader: reader, members: members}, layout: layout,
	}, detected, nil
}

func (factory *Factory) openASAR(
	ctx context.Context, path string, limits importing.ArchiveLimits,
) (*asarCursor, error) {
	archive, detected, err := factory.loadElectronZIP(ctx, path, limits)
	if err != nil {
		return nil, err
	}
	if !detected {
		closeZIP(ctx, factory.reporter, &archive.archive)
		return nil, importing.ErrElectronASARInvalid
	}
	appReader, err := archive.archive.reader.File[archive.layout.AppASAR.Ordinal].Open()
	if err != nil {
		closeZIP(ctx, factory.reporter, &archive.archive)
		return nil, fmt.Errorf("%w: open app.asar", importing.ErrElectronASARInvalid)
	}
	appSize := int64(archive.layout.AppASAR.Header.UncompressedSize64)
	monitor := &archiveReadMonitor{ctx: ctx, reader: appReader, limit: limits.MaxEntryBytes}
	members, dataOffset, err := readASARHeader(monitor, appSize, limits)
	if err != nil {
		closeResource(ctx, factory.reporter, appReader)
		closeZIP(ctx, factory.reporter, &archive.archive)
		return nil, err
	}
	if err := importing.ValidateUnpackedASARMembers(members, archive.layout); err != nil {
		closeResource(ctx, factory.reporter, appReader)
		closeZIP(ctx, factory.reporter, &archive.archive)
		return nil, importing.ErrElectronASARInvalid
	}
	// Packed members are visited by physical offset; unpacked offsets are zero,
	// so their suffix retains the same lexical order as the legacy second pass.
	importing.SortASARMembersByOffset(members)
	return &asarCursor{
		ctx: ctx, reporter: factory.reporter, archive: archive, appReader: appReader, appMonitor: monitor,
		appSize: appSize, dataOffset: dataOffset, members: members,
	}, nil
}
