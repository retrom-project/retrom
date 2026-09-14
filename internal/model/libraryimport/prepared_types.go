package libraryimport

import (
	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/capability/engine/rpgmaker/detector"
	"retrom/internal/capability/format/importing"
)

type PreparedDisposition struct {
	File        ImportFile
	Disposition string
	Reason      string
}

type PreparedSource struct {
	File           ImportFile
	Role           string
	LogicalName    string
	ArchiveBlobID  string
	ArchiveOrdinal *int
	SortOrder      *int
}

type PreparedArchive struct {
	BlobID       string
	Entries      []importing.ArchiveEntry
	Materialized map[int]blobstore.Metadata
}

type PreparedGroup struct {
	Sources             []PreparedSource
	DOSEntries          []PreparedDOSEntry
	DefaultDOSEntry     string
	BundleBlobID        string
	Bundle              *blobstore.Metadata
	ValidationStatus    string
	CompatibilityCode   string
	DependencySnapshot  string
	TitleSource         string
	TitleSourceExplicit bool
	ValidationFiles     []PreparedValidationFile
	ContentKind         string
	GroupKey            string
	MultiEntries        []PreparedMultiDiscEntry
	MultiDependency     *corevalidation.MultiDiscSnapshot
	CanonicalPlaylist   *blobstore.Metadata
	RPGProfile          *detector.Profile
	RPGProjectRoot      string
	RPGRemovedFiles     []string
}

type PreparedMultiDiscEntry struct {
	Ordinal                                             int
	State                                               string
	SourceReference, NormalizedReference, CanonicalName string
	UploadFileID, BlobID, SourceLogicalName             string
}

type PreparedValidationFile struct {
	Artifact                  *blobstore.Metadata
	Role, LogicalName, BlobID string
	SortOrder                 int
}

type PreparedDOSEntry struct {
	Path, Kind             string
	Rank                   int
	Safe                   bool
	BatchContents          []byte
	InferredTerminalTarget bool
}

type PreparedReusableUploadFile struct {
	ID, Path, BlobID string
	Size             int64
}
