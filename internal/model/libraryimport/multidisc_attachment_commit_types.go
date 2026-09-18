package libraryimport

import (
	"context"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/content/multidisc"
	validationservice "retrom/internal/model/corevalidation"
)

// MultiDiscAttachmentFile is the storage metadata needed when an accepted
// attachment becomes a new immutable source snapshot.
type MultiDiscAttachmentFile struct {
	Role, LogicalName, UploadFileID, BlobID, BlobSHA string
	BlobSize                                         int64
	SortOrder                                        int
}

type MultiDiscAttachmentValidation struct {
	Status, CompatibilityCode, DependencySnapshotJSON string
	Files                                             []PreparedValidationFile
}

type MultiDiscAttachmentCommitRequest struct {
	Input                MultiDiscAttachmentInput
	JobID, WorkerID      string
	ExecutionStartedAtMS int64
	BaseFiles            []MultiDiscAttachmentFile
	ResultEntries        []multidisc.Entry
	CanonicalPlaylist    blobstore.Metadata
	ResultManifestJSON   string
	ResultManifestDigest string
	Validation           MultiDiscAttachmentValidation
}

type MultiDiscAttachmentCommitWrite struct {
	MultiDiscAttachmentCommitRequest
	SourceSnapshotID, ValidationID, ConsumptionID, EventID string
	NowMS                                                  int64
}

type MultiDiscAttachmentCommitRepository interface {
	LoadBIOSRecords(context.Context, string, string) ([]validationservice.BIOSRecord, error)
	CommitAccepted(context.Context, MultiDiscAttachmentCommitWrite) error
}

type MultiDiscAttachmentTerminalTarget struct {
	AttachmentID, ItemID, JobID, WorkerID, RequestedByUserID string
	ExecutionStartedAtMS                                     int64
}

type MultiDiscAttachmentActor struct {
	Kind, UserID, Label string
}

type MultiDiscAttachmentRejectRequest struct {
	Target MultiDiscAttachmentTerminalTarget
	Actor  MultiDiscAttachmentActor
	Code   string
	Cause  string
}

type MultiDiscAttachmentRejectWrite struct {
	Target                                       MultiDiscAttachmentTerminalTarget
	Actor                                        MultiDiscAttachmentActor
	Code, DiagnosticsJSON, EvidenceJSON, EventID string
	NowMS                                        int64
}

type MultiDiscAttachmentRetryRequest struct {
	Target MultiDiscAttachmentTerminalTarget
	Code   string
}

type MultiDiscAttachmentRetryWrite struct {
	Target                           MultiDiscAttachmentTerminalTarget
	Code, DiagnosticsJSON, EventJSON string
	NowMS                            int64
}

type MultiDiscAttachmentRetryResult struct {
	Scheduled bool
	Delay     time.Duration
}

type MultiDiscAttachmentTerminalRepository interface {
	Reject(context.Context, MultiDiscAttachmentRejectWrite) error
	TryRetry(context.Context, MultiDiscAttachmentRetryWrite) (MultiDiscAttachmentRetryResult, error)
	FailRetryable(context.Context, MultiDiscAttachmentRetryWrite) error
	SyncCancellation(context.Context, string, int64) error
	FinishCancellation(context.Context, MultiDiscAttachmentCancellationWrite) (bool, error)
}

type MultiDiscAttachmentCancellationRequest struct {
	Target MultiDiscAttachmentTerminalTarget
}

type MultiDiscAttachmentCancellationWrite struct {
	Target    MultiDiscAttachmentTerminalTarget
	NowMS     int64
	EventJSON string
}
