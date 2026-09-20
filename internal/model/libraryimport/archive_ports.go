package libraryimport

import (
	"context"

	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/format/importing"
)

// ArchiveReader is owned by the acquiring Service and borrowed synchronously by CAS.
type ArchiveReader interface {
	Next() (importing.ArchiveMemberHeader, error)
	Read([]byte) (int, error)
	Complete(importing.ArchiveContent) (importing.ArchiveEntry, error)
	Close() error
}

type ProjectArchiveOpener interface {
	OpenProject(context.Context, string, contentprofile.ArchiveFormat, importing.ArchiveLimits) (ArchiveReader, error)
}

type ArchiveInspector interface {
	ScanZIP(context.Context, string, importing.ArchiveLimits) ([]importing.ArchiveEntry, error)
	ValidateNWJSExecutable(context.Context, string) error
	DetectElectronASARZIP(context.Context, string, importing.ArchiveLimits) (bool, error)
}
