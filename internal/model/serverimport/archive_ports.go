package serverimport

import (
	"context"

	"retrom/internal/capability/format/importing"
)

type ArchiveInspector interface {
	ScanZIP(context.Context, string, importing.ArchiveLimits) ([]importing.ArchiveEntry, error)
}
