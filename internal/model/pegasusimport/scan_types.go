package pegasusimport

import "context"

type ScanMetadata struct {
	Path, Digest, Facts, State, ErrorCode string
	Size                                  int64
}
type ScanCollection struct {
	ID, MetadataPath, Name, Description, IgnoredJSON, WarningJSON string
	ShortName                                                     *string
	SegmentOrdinal, GameCount, IssueCount                         int64
}
type ScanItem struct {
	ID, CollectionID, MetadataPath, SourceKey, Title          string
	GameOrdinal                                               int64
	DiscoveryState, DiscoveryCode, MetadataJSON, WarningsJSON string
	SourceManifestJSON, SourceManifestDigest                  string
	Files                                                     []ScanFile
	Assets                                                    []ScanAsset
}
type ScanFile struct {
	Ordinal           int64
	Kind, Path, Facts string
	Size              int64
}
type ScanAsset struct {
	Kind, Method, Path, Facts, MediaType string
	Size                                 int64
	Width, Height                        *int64
}
type ScanHeaders struct {
	Metadata    []ScanMetadata
	Collections []ScanCollection
}
type ScanShape struct {
	Metadata, InvalidMetadata, Collections, Items, Blocked, Covers, Videos, EstimatedBytes int64
}
type ScanSummary struct {
	Shape          ScanShape
	SnapshotDigest string
	MediaWarnings  int64
}
type ScanProjection struct {
	Headers ScanHeaders
	Items   []ScanItem
	Summary ScanSummary
}
type ScanLease struct {
	Before ExecutionSnapshot
	NowMS  int64
}

type ScanRepository interface {
	LoadScanOwner(context.Context, string) (ExecutionSnapshot, error)
	LoadScanShape(context.Context, string) (ScanShape, error)
	CommitScanHeaders(context.Context, ScanLease, ScanHeaders) error
	CommitScanItems(context.Context, ScanLease, []ScanItem) error
	CommitScanFinish(context.Context, ScanLease, ScanSummary) error
}
