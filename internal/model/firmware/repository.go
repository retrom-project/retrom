package firmware

import (
	"context"

	"retrom/internal/capability/format/importing"
)

// BrowserInstallCommand captures all inputs for a browser BIOS installation.
type BrowserInstallCommand struct {
	RequirementID      string
	FileID             string
	Version            int64
	PreparedSourceKind string
	PreparedFileKind   string
	PreparedBlobID     string
	PreparedSHA256     string
	ArchiveEntries     []importing.ArchiveEntry
	NowMS              int64
}

// ServerInstallCommand wraps a ServerInstallRequest with timing for atomic commit.
type ServerInstallCommand struct {
	Request ServerInstallRequest
	NowMS   int64
}

// InstallFacts contains the pre-validated inputs needed before committing a browser install.
type InstallFacts struct {
	SourceKind string
	FileKind   string
	BlobID     string
	SHA256     string
}

type Repository interface {
	LoadInstallFacts(ctx context.Context, requirementID string, expectedVersion int64, fileID string) (InstallFacts, error)
	LoadArchiveInspection(ctx context.Context, requirementID string) (ArchiveInspection, error)
	CommitBrowserInstall(context.Context, BrowserInstallCommand) (Installation, error)
	CommitServerInstall(context.Context, ServerInstallCommand) (ServerInstallResult, error)
}
type ReleaseSignal interface{ Signal() }

type Requirement struct {
	ID, SourceKind, FileKind, LogicalName              string
	ProviderID, TargetID, SourceVersion, CatalogDigest string
	Enabled                                            bool
	Version                                            int64
	Size                                               *int64
	MD5, SHA1, SHA256, ArchiveMembersJSON              *string
}
type Upload struct {
	ID, SessionID, RelativePath, State, BlobID, MD5, SHA1, SHA256 string
	Size                                                          int64
}
type ActiveInstallation struct {
	ID, BlobID, Filename, MD5, SHA1, SHA256, Status string
	Size, ValidatedVersion                          int64
}
type InstallationWrite struct {
	ID, RequirementID, BlobID, Filename, MD5, SHA1, SHA256, Status, SourceKind string
	Size, RequirementVersion, AtMS                                             int64
	DetailsJSON                                                                []byte
	CandidateID                                                                *string
}
type Consumption struct {
	ID, UploadID, FileID, InstallationID string
	AtMS                                 int64
}
type Selection struct {
	CandidateID, ImportID, RequirementID string
	AtMS                                 int64
}
type ServerOutcome struct {
	ImportID, RequirementID, JobID, MatchMethod string
	Result                                      ServerInstallResult
	Code                                        string
	DetailsJSON, EventJSON                      []byte
	AtMS                                        int64
}
