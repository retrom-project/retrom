package sourceimport

import (
	"context"
	"fmt"
	"io"

	"retrom/internal/cleanup"
	"retrom/internal/serversource"
	application "retrom/internal/service/sourceimport"
)

func (source scanSource) Disc(ctx context.Context, file application.DiscoveredFile) ([]byte, error) {
	release, err := source.acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire disc reader: %w", err)
	}
	defer release()
	handle, before, err := serversource.OpenRelativeFile(source.root.path, source.selectedPath, file.Path)
	if err != nil {
		return nil, fmt.Errorf("open disc: %w: %w", ErrSourceChanged, err)
	}
	defer func() { cleanup.Error("close disc", handle.Close()) }()
	if before.Size() != file.Size || serversource.FactsDigest(before) != file.Facts {
		return nil, ErrSourceChanged
	}
	header := make([]byte, 8)
	if _, err := io.ReadFull(contextReader{ctx: ctx, reader: handle}, header); err != nil {
		return nil, fmt.Errorf("read disc: %w", err)
	}
	after, err := handle.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat disc: %w", err)
	}
	if !serversource.SameFileFacts(before, after) {
		return nil, ErrSourceChanged
	}
	return header, nil
}
