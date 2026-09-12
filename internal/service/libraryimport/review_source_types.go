package libraryimport

import "context"

type ReviewSourceReader interface {
	Files(context.Context, string) ([]ReviewSourceRecord, error)
	ArchiveEntries(context.Context, string) (ReviewArchive, error)
}
type ReviewSourceRecord struct {
	ID, Name, SHA256, MD5, CRC32 string
	SizeBytes                    int64
	Archive                      bool
	ArchiveBlobID                *string
}
type ReviewSourceFile struct {
	ID             string               `json:"uploadFileId"`
	Name           string               `json:"name"`
	SizeBytes      int64                `json:"sizeBytes"`
	SHA256         string               `json:"sha256"`
	MD5            string               `json:"md5"`
	CRC32          string               `json:"crc32"`
	Archive        bool                 `json:"archive"`
	ArchiveFormat  *string              `json:"archiveFormat"`
	ArchiveEntries []ReviewArchiveEntry `json:"archiveEntries"`
}
type ReviewArchive struct {
	Format  *string
	Entries []ReviewArchiveEntry
}
type ReviewArchiveEntry struct {
	CRC32     string `json:"crc32"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
}
type ReviewRPGMaker struct {
	SelectedCoreID          string                 `json:"selectedCoreId"`
	Generation              string                 `json:"generation"`
	EvidenceGeneration      *string                `json:"evidenceGeneration"`
	EvidenceConfidence      string                 `json:"evidenceConfidence"`
	SelfContained           bool                   `json:"selfContained"`
	SelfContainedOverride   bool                   `json:"selfContainedOverride"`
	ExternalRTPRequirements []ReviewRTPDeclaration `json:"externalRTPRequirements"`
}
type ReviewRTPDeclaration struct {
	Slot         int64  `json:"slot"`
	DeclaredName string `json:"declaredName"`
}
