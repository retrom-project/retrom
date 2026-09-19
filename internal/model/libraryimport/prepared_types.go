package libraryimport

import (
	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/capability/engine/rpgmaker/detector"
	"retrom/internal/capability/format/importing"
	blobmodel "retrom/internal/model/blob"
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
	Materialized map[int]blobmodel.PreparedBlob
}

type PreparedGroup struct {
	Sources             []PreparedSource
	DOSEntries          []PreparedDOSEntry
	DefaultDOSEntry     string
	BundleBlobID        string
	Bundle              *blobmodel.PreparedBlob
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
	CanonicalPlaylist   *blobmodel.PreparedBlob
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
	Artifact                  *blobmodel.PreparedBlob
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
