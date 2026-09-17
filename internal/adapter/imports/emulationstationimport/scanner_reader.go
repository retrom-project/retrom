package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	emulationstationimportservice "retrom/internal/service/emulationstationimport"

	"retrom/internal/adapter/files/serversource"
	"retrom/internal/foundation/cleanup"
	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
)

func readFrozenFile(
	ctx context.Context,
	root Root,
	selectedPath string,
	entry emulationstationimportservice.DiscoveredFile,
	maximum int64,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("emulationstationimport/scan cancelled: %w", err)
	}
	release, err := serversource.AcquireReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("emulationstationimport/acquire scan reader: %w", err)
	}
	defer release()
	handle, before, err := serversource.OpenRelativeFile(root.path, selectedPath, entry.Path)
	if err != nil {
		return nil, classifyFrozenOpenError(err)
	}
	if before.Size() != entry.Size || serversource.FactsDigest(before) != entry.Facts {
		if handle != nil {
			cleanup.Error("close", handle.Close())
		}
		return nil, emulationstationimportmodel.ErrSourceChanged
	}
	contents, readErr := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: handle}, maximum+1))
	after, statErr := handle.Stat()
	cleanup.Error("close", handle.Close())
	if readErr != nil {
		return nil, fmt.Errorf(
			"emulationstationimport/read frozen source: %w: %w",
			serversource.ErrRootUnavailable,
			readErr,
		)
	}
	if statErr != nil {
		return nil, fmt.Errorf(
			"emulationstationimport/stat frozen source: %w: %w",
			serversource.ErrRootUnavailable,
			statErr,
		)
	}
	if int64(len(contents)) != entry.Size ||
		!serversource.SameFileFacts(before, after) || len(contents) > int(maximum) {
		return nil, emulationstationimportmodel.ErrSourceChanged
	}
	return contents, nil
}

func classifyFrozenOpenError(err error) error {
	if errors.Is(err, serversource.ErrRootUnavailable) {
		return fmt.Errorf("EmulationStation frozen root unavailable: %w", err)
	}
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, serversource.ErrPathInvalid) ||
		errors.Is(err, serversource.ErrSourceChanged) {
		return fmt.Errorf(
			"open changed EmulationStation frozen source: %w: %w",
			emulationstationimportmodel.ErrSourceChanged,
			err,
		)
	}
	return fmt.Errorf("emulationstationimport/open frozen source: %w: %w", serversource.ErrRootUnavailable, err)
}
