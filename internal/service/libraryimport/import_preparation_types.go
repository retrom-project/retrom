package libraryimport

import (
	"context"

	"retrom/internal/content/arcade"

	"retrom/internal/core/scummvm"
)

type ImportPreparationOptions struct {
	MultiDiscEnabled, MetadataScraperAvailable bool
	ScummVMDetector                            *scummvm.Detector
}

type PreparedImport struct {
	Request                               ImportRequest
	Upload                                ImportUpload
	Target                                ImportTarget
	ContentMode, SourceType, DATVersionID string
	Files                                 []ImportFile
	Dispositions                          []PreparedDisposition
	Groups                                []PreparedGroup
	Archives                              []PreparedArchive
}

type ImportPreparationCatalog interface {
	arcade.Catalog
	ActiveDAT(context.Context, string, string) (string, error)
}
