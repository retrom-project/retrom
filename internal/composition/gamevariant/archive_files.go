package gamevariant

import (
	"context"
	"fmt"

	"retrom/internal/content/arcade"
	"retrom/internal/filestore"
	"retrom/internal/importing"
)

type archiveFiles struct{ store *filestore.Store }

func (files archiveFiles) Entries(ctx context.Context, record string) ([]importing.ArchiveEntry, error) {
	if files.store == nil {
		return nil, arcade.ErrInvalid
	}
	entries, err := importing.ScanZIP(ctx, files.store.Path(record), importing.DefaultArchiveLimits())
	if err != nil {
		return nil, fmt.Errorf("read published arcade archive: %w", err)
	}
	return entries, nil
}
