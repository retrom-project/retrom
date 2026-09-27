package firmware

import (
	"context"

	"retrom/internal/firmware"
	"retrom/internal/importing"
)

type Repository interface {
	WithRead(context.Context, func(ReadScope) error) error
	WithWrite(context.Context, func(WriteScope) error) error
}
type ReadScope struct {
	Requirements  RequirementRecords
	Uploads       UploadReader
	Installations InstallationReader
	Archives      ArchiveReader
}
type WriteScope struct {
	ReadScope
	Archives      ArchiveWriter
	Installations InstallationWriter
	Retirements   SupersessionScope
	Server        ServerRecords
}
type RequirementRecords interface {
	Get(context.Context, string) (Requirement, bool, error)
	DATEntries(context.Context, string) ([]firmware.ExpectedDATEntry, error)
}
type UploadReader interface {
	Get(context.Context, string) (Upload, bool, error)
}
type InstallationReader interface {
	Active(context.Context, string) (ActiveInstallation, bool, error)
}
type ArchiveReader interface {
	Entries(context.Context, string) ([]importing.ArchiveEntry, error)
}
type ArchiveWriter interface {
	Put(context.Context, string, []importing.ArchiveEntry, int64) error
}
type InstallationWriter interface {
	Create(context.Context, InstallationWrite) error
	Consume(context.Context, Consumption) error
}
type ServerRecords interface {
	LockExecution(context.Context, ServerExecution) error
	SelectCandidate(context.Context, Selection) error
	Finish(context.Context, ServerOutcome) error
}
type ServerExecution struct {
	ImportID, JobID, WorkerID string
	ExecutionNo, AtMS         int64
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
	ID, SessionID, RelativePath, State, FileRecord, MD5, SHA1, SHA256 string
	Size                                                              int64
}
type ActiveInstallation struct {
	ID, FileRecord, Filename, MD5, SHA1, SHA256, Status string
	Size, ValidatedVersion                              int64
}
type InstallationWrite struct {
	UploadSessionID                                                                string
	ID, RequirementID, FileRecord, Filename, MD5, SHA1, SHA256, Status, SourceKind string
	Size, RequirementVersion, AtMS                                                 int64
	DetailsJSON                                                                    []byte
	CandidateID                                                                    *string
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
