package libraryimport

import (
	"context"

	"retrom/internal/capability/format/importing"
	"retrom/internal/capability/security/authn"
)

// ArcadeParentCommitCandidate is the immutable input captured when a parent
// attachment worker claims its job. The persistence adapter rechecks these
// values inside the commit transaction before writing any evidence.
type ArcadeParentCommitCandidate struct {
	AttachmentID, ItemID, DraftID, BaseSnapshotID string
	Machine, ProviderID, TargetID, DATID          string
	UploadFileID, UploadSessionID                 string
	BlobID, BlobSHA                               string
	BlobSize                                      int64
	ContentPolicyDigest                           string
}

type ArcadeParentSourceFile struct {
	Role, LogicalName, UploadFileID, BlobID, BlobSHA string
	BlobSize                                         int64
	ArchiveBlobID                                    *string
	ArchiveOrdinal                                   *int
	SortOrder                                        int
}

type ArcadeParentValidationFile struct {
	Role, LogicalName, BlobID string
	SortOrder                 int
}

type ArcadeParentValidation struct {
	Status, CompatibilityCode, DependencySnapshot string
	Files                                         []ArcadeParentValidationFile
}

type ArcadeParentAcceptedCommit struct {
	Candidate                     ArcadeParentCommitCandidate
	JobID, WorkerID               string
	Entries                       []importing.ArchiveEntry
	Files                         []ArcadeParentSourceFile
	ManifestJSON                  string
	ManifestDigest                string
	Validation                    ArcadeParentValidation
	DiagnosticsJSON, EvidenceJSON string
	Actor                         authn.Actor
	NowMS                         int64
}

type ArcadeParentRejectedCommit struct {
	AttachmentID, ItemID, JobID, WorkerID string
	Code                                  string
	DiagnosticsJSON                       string
	EvidenceJSON                          string
	BlobSize                              int64
	BlobSHA                               string
	Actor                                 authn.Actor
	NowMS                                 int64
}

type ArcadeParentRetryableCommit struct {
	AttachmentID, ItemID, JobID, WorkerID string
	Code                                  string
	DiagnosticsJSON                       string
	BlobSize                              int64
	BlobSHA                               string
	NowMS                                 int64
}

type ArcadeParentCancellationSync struct {
	JobID string
	NowMS int64
}

type ArcadeParentAttachmentCancellation struct {
	AttachmentID, ItemID, JobID, WorkerID string
	NowMS                                 int64
}

// ArcadeParentCommitRepository contains the caller-visible persistence
// operations for accepted and terminal parent attachment worker states.
// Implementations own each SQL transaction; application code supplies the
// worker's already prepared evidence and keeps orchestration outside storage.
type ArcadeParentCommitRepository interface {
	CommitAccepted(context.Context, ArcadeParentAcceptedCommit) error
	FinishRejected(context.Context, ArcadeParentRejectedCommit) error
	FinishRetryable(context.Context, ArcadeParentRetryableCommit) error
	SyncCancellation(context.Context, ArcadeParentCancellationSync) error
	FinishCancellation(context.Context, ArcadeParentAttachmentCancellation) (bool, error)
}
