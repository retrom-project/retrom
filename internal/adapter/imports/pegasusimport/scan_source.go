package pegasusimport

import (
	"context"
	"errors"
	"fmt"
	"io"

	"retrom/internal/adapter/files/serversource"
	"retrom/internal/capability/format/pegasusmeta"
	"retrom/internal/foundation/cleanup"
	application "retrom/internal/service/pegasusimport"
)

type scanSource struct {
	root         Root
	selectedPath string
	acquire      func(context.Context) (func(), error)
}

func (source scanSource) Discover(ctx context.Context, visit func(application.DiscoveredFile) error) error {
	release, err := source.acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire Pegasus discovery reader: %w", err)
	}
	defer release()
	directory, err := serversource.OpenSelectedDirectory(source.root.path, source.selectedPath)
	if err != nil {
		return fmt.Errorf("%w: %w", application.ErrRootUnavailable, errors.Join(serversource.ErrRootUnavailable, err))
	}
	defer func() { cleanup.Error("close Pegasus scan directory", directory.Close()) }()
	_, err = serversource.WalkFilesContext(ctx, directory,
		serversource.Limits{MaxDepth: 64, MaxDirectories: 250000, MaxFiles: 2000000},
		func(candidate serversource.File) error {
			file, found := discoveredScanFacts(candidate)
			if !found {
				return nil
			}
			return visit(file)
		})
	if errors.Is(err, serversource.ErrScanLimit) {
		return fmt.Errorf("%w: %w", ErrScanLimit, err)
	}
	if err != nil {
		return fmt.Errorf("walk Pegasus source: %w", err)
	}
	return nil
}

func (source scanSource) Metadata(ctx context.Context, file application.DiscoveredFile) ([]byte, error) {
	if file.Size < 0 || file.Size > pegasusmeta.MaxMetadataBytes {
		return nil, pegasusmeta.ErrTooLarge
	}
	release, err := source.acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire Pegasus metadata reader: %w", err)
	}
	defer release()
	handle, before, err := serversource.OpenRelativeFile(source.root.path, source.selectedPath, file.Path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSourceChanged, err)
	}
	defer func() { cleanup.Error("close Pegasus metadata source", handle.Close()) }()
	if before.Size() != file.Size || serversource.FactsDigest(before) != file.Facts {
		return nil, ErrSourceChanged
	}
	contents, err := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: handle}, pegasusmeta.MaxMetadataBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Pegasus metadata: %w", err)
	}
	after, err := handle.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat Pegasus metadata: %w", err)
	}
	if int64(len(contents)) != file.Size || !serversource.SameFileFacts(before, after) {
		return nil, ErrSourceChanged
	}
	return contents, nil
}

// Entries that disappear, change type or cannot be opened are omitted from the
// regular-file index, preserving the scanner's existing discovery policy.
func discoveredScanFacts(candidate serversource.File) (application.DiscoveredFile, bool) {
	handle, info, err := serversource.OpenFile(candidate)
	if err != nil {
		return application.DiscoveredFile{}, false
	}
	cleanup.Error("close Pegasus discovered file", handle.Close())
	return application.DiscoveredFile{
		Path: candidate.RelativePath, Name: candidate.Basename,
		Size: info.Size(), Facts: serversource.FactsDigest(info),
	}, true
}
