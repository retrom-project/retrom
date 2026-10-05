package libraryimport

import (
	"retrom/internal/content/diagnostic"
	"retrom/internal/content/requirements"
	"retrom/internal/core/rpgmaker/detector"
	corevalidation "retrom/internal/core/validation"
	"retrom/internal/filestore"
	"retrom/internal/importing"
)

type PreparedDisposition struct {
	File        ImportFile
	Disposition string
	Reason      string
	Rejection   *diagnostic.Rejection
}

type PreparedSource struct {
	Payload           *filestore.Metadata
	File              ImportFile
	Role              string
	LogicalName       string
	ArchiveFileRecord string
	ArchiveOrdinal    *int
	SortOrder         *int
}

type PreparedArchive struct {
	FileRecord   string
	Entries      []importing.ArchiveEntry
	Materialized map[int]filestore.Metadata
}

type PreparedGroup struct {
	ContentFacts        *requirements.Facts
	ItemID              string
	Sources             []PreparedSource
	DOSEntries          []PreparedDOSEntry
	DefaultDOSEntry     string
	BundleFileRecord    string
	Bundle              *filestore.Metadata
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
	CanonicalPlaylist   *filestore.Metadata
	RPGProfile          *detector.Profile
	RPGProjectRoot      string
	RPGRemovedFiles     []string
}

type PreparedMultiDiscEntry struct {
	Ordinal                                             int
	State                                               string
	SourceReference, NormalizedReference, CanonicalName string
	UploadFileID, FileRecord, SourceLogicalName         string
}

type PreparedValidationFile struct {
	Artifact                      *filestore.Metadata
	Role, LogicalName, FileRecord string
	SortOrder                     int
}

type PreparedDOSEntry struct {
	Path, Kind             string
	Rank                   int
	Safe                   bool
	BatchContents          []byte
	InferredTerminalTarget bool
}

type PreparedReusableUploadFile struct {
	ID, Path, FileRecord string
	Size                 int64
}
