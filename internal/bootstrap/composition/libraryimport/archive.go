package libraryimport

import (
	"log/slog"

	archiveadapter "retrom/internal/adapter/content/archive"
	cleanupadapter "retrom/internal/adapter/system/cleanup"
	model "retrom/internal/model/libraryimport"
)

// NewArchiveInspector assembles the archive reader used by the legacy import worker.
func NewArchiveInspector() model.ArchiveInspector {
	return archiveadapter.New(cleanupadapter.NewReporter(slog.Default()))
}
