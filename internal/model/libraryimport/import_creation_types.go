package libraryimport

import (
	"context"

	blobmodel "retrom/internal/model/blob"

	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/capability/security/authn"
	"retrom/internal/model/importprogress"
	"retrom/internal/model/metadatascrape"
	"retrom/internal/model/payloadrelease"
	"retrom/internal/model/tagging"
)

type ImportCreationResult struct {
	Created ServerCreated
	Owned   ServerImportResult
}
type ImportCreationOptions struct {
	ReviewHandoffKind string
	Queued            *QueuedImportExecution
	Source            *OwnedImportCreation
	Reconfiguration   *ImportReconfiguration
}
type OwnedImportCreation struct {
	Intent SourceCreationIntent
	Before SourceCreationSnapshot
}
type ImportReconfiguration struct {
	ImportID string
	Version  int64
	FileIDs  []string
}
type QueuedImportExecution struct {
	ImportID, JobID, WorkerID, ActorUserID        string
	ExecutionNo, Attempt, StartedAtMS, DeadlineMS int64
	Target                                        ImportTargetSnapshot
}
type CreationQueuedSnapshot struct {
	Execution                                                                   QueuedImportExecution
	JobState, ImportState, RequestJSON, RequestDigest, TargetJSON, TargetDigest string
	ParentVersion, JobVersion, LeaseUntilMS, UploadVersion                      int64
	UploadID, UploadDigest                                                      string
	MaxAttempts                                                                 int64
}
type ImportCreationRepository interface {
	WithCreation(context.Context, func(ImportCreationScope) error) error
}
type ImportCreationScope struct {
	Facts      ImportFactsReader
	Headers    CreationHeaderWriter
	Sources    CreationSourceWriter
	Reviews    CreationReviewWriter
	Finish     CreationCompletionWriter
	BIOS       CreationBIOSReader
	Arcade     CreationArcadeReader
	Duplicates ContentDuplicateReader
	Claims     ApprovalDecisionWriter
	Tags       tagging.WriteScope
	Metadata   metadatascrape.ScheduleScope
	Payload    payloadrelease.SchedulingScope
	Ownership  SourceOwnershipRecords
	Results    CreationResultsReader
}
type CreationHeaderWriter interface {
	Queued(context.Context, string) (CreationQueuedSnapshot, error)
	FenceInputs(context.Context, PreparedImport) error
	Header(context.Context, CreationHeader) error
}
type CreationSourceWriter interface {
	Archive(context.Context, PreparedArchive, int64) (map[int]string, error)
	Artifact(context.Context, CreationArtifact) (string, error)
	Source(context.Context, CreationSource) error
	Duplicate(context.Context, CreationDuplicate) error
}
type CreationReviewWriter interface {
	Validation(context.Context, CreationValidation) error
	Draft(context.Context, CreationDraft) error
	RPG(context.Context, CreationRPGProfile) error
	Events(context.Context, []CreationEvent) error
}
type CreationCompletionWriter interface {
	Aggregate(context.Context, CreationAggregate) error
	FinishJob(context.Context, CreationJobFinish) error
	Reconfiguration(context.Context, string) (CreationReconfigurationHead, error)
	ResolveFiles(context.Context, CreationFileResolution) error
}
type CreationArcadeReader interface {
	BIOS(context.Context, string, string, string) (corevalidation.BIOSDependency, bool, error)
}
type CreationHeader struct {
	ImportID, JobID, ConsumptionID, ConfigJSON, ConfigDigest, DedupeKey string
	Plan                                                                PreparedImport
	State, ItemState                                                    string
	Running, Pending, Ignored, Rejected                                 int64
	CompletedAtMS                                                       *int64
	Queued                                                              *CreationQueuedSnapshot
	NowMS                                                               int64
}
type CreationArtifact struct {
	Metadata  blobmodel.PreparedBlob
	MediaType string
	NowMS     int64
}
type CreationSourceFile struct {
	PreparedSource
	BlobID string
	Order  int
}
type CreationSource struct {
	ItemID         string
	ImportID       string
	SnapshotID     string
	ContentKind    string
	GroupKey       string
	State          string
	HandoffKind    string
	ManifestJSON   string
	ManifestDigest string
	SearchText     string
	Files          []CreationSourceFile
	Discs          []PreparedMultiDiscEntry
	NowMS          int64
}
type CreationDuplicate struct {
	ItemID, Identity string
	Matches          []DuplicateGame
	NowMS            int64
}
type CreationValidation struct {
	ID, ItemID, SnapshotID, ManifestDigest, InputDigest string
	Status, Code, DependencyJSON, DATID, DefaultDOS     string
	Target                                              ImportTarget
	DOSEntries                                          []PreparedDOSEntry
	Files                                               []PreparedValidationFile
	NowMS                                               int64
}
type CreationDraft struct {
	ID, ItemID, TargetID, SnapshotID, MetadataJSON, SearchText string
	SelectedValidationID, DefaultDOS                           *string
	NowMS                                                      int64
}
type CreationRPGProfile struct {
	DraftID, Generation, EvidenceFamily, EvidenceConfidence, RequirementsDigest, AnalysisJSON, FilesDigest string
	ProviderID, TargetID, DependencyDigest                                                                 string
	EvidenceGeneration, EngineVersion, EntryHTML                                                           *string
	FileCount                                                                                              int
	TotalBytes, NowMS                                                                                      int64
}
type CreationEvent struct {
	JobID, ScopeType, ScopeID, Kind, DataJSON string
	NowMS                                     int64
}
type CreationAggregate struct {
	ImportID                                                  string
	ExpectedVersion, ExpectedPending                          int64
	Running, Pending, Discarded, ImportedItems, ImportedFiles int64
	Projection                                                importprogress.Projection
	NowMS                                                     int64
}
type CreationJobFinish struct {
	Before CreationQueuedSnapshot
	NowMS  int64
}
type CreationReconfigurationHead struct {
	ImportID string
	Version  int64
	Progress importprogress.Snapshot
	Files    []string
}
type CreationFileResolution struct {
	Before        CreationReconfigurationHead
	ReplacementID string
	FileIDs       []string
	Actor         authn.Actor
	Projection    importprogress.Projection
	NowMS         int64
}

type CreationResultsReader interface {
	Read(context.Context, ServerCreated) (ServerImportResult, error)
}
