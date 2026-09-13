package emulationstationimport

import "retrom/internal/capability/format/emulationstationmeta"

type ScanGamelist struct {
	Path, Digest, Facts, State, ErrorCode string
	Size                                  int64
	Document                              emulationstationmeta.Document
}

type ScanCollection struct {
	ID, GamelistPath, RelativeDirectory, DisplayName string
	GameCount, IssueCount, FolderEntryCount          int64
	HiddenGameCount, AdultGameCount                  int64
	ExtensionSummaryJSON                             string
	ExtensionOtherCount                              int64
}

type ScanItem struct {
	ID, CollectionID, GamelistPath, SourceKey, Title string
	GameOrdinal                                      int64
	SourceFlagsJSON                                  string
	DiscoveryState, DiscoveryCode                    string
	ContentKind, MetadataJSON, WarningsJSON          string
	SourceManifestJSON, SourceManifestDigest         string
	Files                                            []ScanItemFile
	Assets                                           []ScanAsset
}

type ScanItemFile struct {
	Ordinal           int64
	Kind, Path, Facts string
	Size              int64
}

type ScanAsset struct {
	Kind, Method, Path, State, WarningCode string
	Facts, MediaType                       *string
	Size, Width, Height                    *int64
}

type ScanProjection struct {
	Gamelists                              []ScanGamelist
	Collections                            []ScanCollection
	Items                                  []ScanItem
	SnapshotDigest                         string
	EstimatedBytes                         int64
	InvalidGamelists, FolderEntries        int64
	Blocked, MediaWarnings, Covers, Videos int64
}
