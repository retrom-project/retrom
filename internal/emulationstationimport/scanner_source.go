package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"io"

	"retrom/internal/cleanup"
	"retrom/internal/serversource"
	application "retrom/internal/service/emulationstationimport"
)

type scannerSource struct {
	root         Root
	selectedPath string
}

func (source scannerSource) Discover(ctx context.Context, visit func(application.DiscoveredFile) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("discover EmulationStation source: %w", err)
	}
	directory, err := serversource.OpenSelectedDirectory(source.root.path, source.selectedPath)
	if err != nil {
		return fmt.Errorf("open EmulationStation selected directory: %w: %w", serversource.ErrRootUnavailable, err)
	}
	defer func() { cleanup.Error("close", directory.Close()) }()
	limits := serversource.Limits{MaxDepth: 64, MaxDirectories: 250_000, MaxFiles: 2_000_000}
	_, err = serversource.WalkFilesContext(ctx, directory, limits, func(candidate serversource.File) error {
		entry, err := discoverScanFile(ctx, candidate)
		if err != nil {
			return err
		}
		return visit(entry)
	})
	if errors.Is(err, serversource.ErrScanLimit) {
		return fmt.Errorf("EmulationStation discovery limit: %w: %w", ErrScanLimit, err)
	}
	if err != nil {
		return fmt.Errorf("walk EmulationStation source: %w", err)
	}
	return nil
}

func discoverScanFile(ctx context.Context, candidate serversource.File) (application.DiscoveredFile, error) {
	release, err := serversource.AcquireReader(ctx)
	if err != nil {
		return application.DiscoveredFile{}, fmt.Errorf("acquire EmulationStation discovery reader: %w", err)
	}
	defer release()
	handle, info, err := serversource.OpenFile(candidate)
	if err != nil {
		return application.DiscoveredFile{}, fmt.Errorf(
			"open EmulationStation discovered source: %w: %w",
			serversource.ErrRootUnavailable,
			err,
		)
	}
	defer func() { cleanup.Error("close", handle.Close()) }()
	return application.DiscoveredFile{
		Path:  candidate.RelativePath,
		Name:  candidate.Basename,
		Size:  info.Size(),
		Facts: serversource.FactsDigest(info),
	}, nil
}

func (source scannerSource) Read(ctx context.Context, file application.DiscoveredFile, maximum int64) ([]byte, error) {
	contents, err := readFrozenFile(ctx, source.root, source.selectedPath, file, maximum)
	return contents, err
}

func (source scannerSource) Disc(ctx context.Context, file application.DiscoveredFile) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("inspect EmulationStation disc: %w", err)
	}
	release, err := serversource.AcquireReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire EmulationStation disc reader: %w", err)
	}
	defer release()
	handle, before, err := serversource.OpenRelativeFile(source.root.path, source.selectedPath, file.Path)
	if err != nil {
		return nil, fmt.Errorf("open EmulationStation disc: %w: %w", ErrSourceChanged, err)
	}
	defer func() { cleanup.Error("close", handle.Close()) }()
	if before.Size() != file.Size || serversource.FactsDigest(before) != file.Facts {
		return nil, ErrSourceChanged
	}
	header := make([]byte, 8)
	_, readErr := io.ReadFull(&contextReader{ctx: ctx, reader: handle}, header)
	after, statErr := handle.Stat()
	if readErr != nil || statErr != nil {
		return nil, fmt.Errorf("read EmulationStation disc header: %w: %w", ErrSourceChanged, errors.Join(readErr, statErr))
	}
	if !serversource.SameFileFacts(before, after) {
		return nil, ErrSourceChanged
	}
	return header, nil
}
