package libraryimport

import (
	"context"
	"errors"
	"time"

	"retrom/internal/authn"
	"retrom/internal/contentcapability"
)

const ArcadeParentAttachmentDeadline = 30 * time.Minute

// The admission records are deliberately expressed as plain values.  The
// application layer does not need to know whether an optional DAT came from a
// nullable SQL column or from another persistence implementation.
type ArcadeParentAttachmentDraft struct {
	DraftID, ItemState, TargetID, EffectiveSnapshotID string
	PlatformID, CoreID, ProviderID, RuntimeTargetID   string
	ContentPolicy                                     contentcapability.Policy
	ActiveDATVersionID                                string
	HasActiveDAT                                      bool
	DraftVersion, PlatformVersion                     int64
}

type ArcadeParentAttachmentValidation struct {
	TargetPlatformInstanceID, CoreID, ProviderID, TargetID string
	DATVersionID, SourceSnapshotID, DependencySnapshotJSON string
	HasDATVersion                                          bool
}

type ArcadeParentAttachmentUpload struct {
	UploadSessionID, SessionState, FileState string
	RelativePath, BlobID, BlobSHA            string
	BlobSize                                 int64
	WholeSessionConsumed                     bool
}

// ArcadeParentAttachmentInput is the frozen worker input persisted with the
// queued job.  It is shared by the admission and worker ports so an adapter
// cannot silently change the wire shape between those operations.
type ArcadeParentAttachmentInput struct {
	SchemaVersion        int    `json:"schemaVersion"`
	AttachmentID         string `json:"attachmentId"`
	ImportItemID         string `json:"importItemId"`
	ReviewDraftID        string `json:"reviewDraftId"`
	BaseSourceSnapshotID string `json:"baseSourceSnapshotId"`
	DependencyMachine    string `json:"dependencyMachine"`
	ProviderID           string `json:"providerId"`
	TargetID             string `json:"targetId"`
	ContentPolicyDigest  string `json:"contentPolicyDigest"`
	DATVersionID         string `json:"datVersionId"`
	UploadFileID         string `json:"uploadFileId"`
}

type ArcadeParentAttachmentWrite struct {
	Input                  ArcadeParentAttachmentInput
	InputJSON, InputDigest string
	DedupeKey              string
	AttachmentID, JobID    string
	ItemID, DraftID        string
	BaseSourceSnapshotID   string
	DependencyMachine      string
	RequiredByMachine      string
	Depth                  int
	ProviderID, TargetID   string
	DATVersionID, UploadID string
	OriginalFilename       string
	ExpectedDraftVersion   int64
	NowMS                  int64
	Actor                  authn.Actor
}

type ArcadeParentAttachmentAdmissionReader interface {
	ArcadeRelationReader
	Draft(context.Context, string) (ArcadeParentAttachmentDraft, bool, error)
	Validation(context.Context, string, string) (ArcadeParentAttachmentValidation, bool, error)
	Upload(context.Context, string) (ArcadeParentAttachmentUpload, bool, error)
	HasActive(context.Context, string) (bool, error)
}

type ArcadeParentAttachmentAdmissionWriter interface {
	Create(context.Context, ArcadeParentAttachmentWrite) error
}

type ArcadeParentAttachmentAdmissionScope struct {
	Read  ArcadeParentAttachmentAdmissionReader
	Write ArcadeParentAttachmentAdmissionWriter
}

type ArcadeParentAttachmentAdmissionRepository interface {
	WithAdmission(context.Context, func(ArcadeParentAttachmentAdmissionScope) error) error
}

// ErrArcadeParentAttachmentActive lets a persistence adapter preserve the
// stable in-progress error without leaking an index or driver name upwards.
var ErrArcadeParentAttachmentActive = errors.New("arcade parent attachment is already active")

type ArcadeParentAttachmentCandidate struct {
	AttachmentID, ItemID, DraftID, BaseSnapshotID    string
	Machine, RequiredBy, ProviderID, TargetID, DATID string
	UploadFileID, UploadSessionID, OriginalName      string
	BlobID, BlobSHA                                  string
	BlobSize                                         int64
	ContentPolicyDigest                              string
	Depth                                            int
}

type ArcadeParentAttachmentWorkerClaim struct {
	Candidate            ArcadeParentAttachmentCandidate
	Input                ArcadeParentAttachmentInput
	JobID, WorkerID      string
	ExecutionStartedAtMS int64
}

type ArcadeParentSourceSnapshotFile struct {
	Role, LogicalName, UploadFileID, BlobID, BlobSHA string
	BlobSize                                         int64
	SourceArchiveBlobID, SourceArchiveSHA            string
	SourceArchiveEntryOrdinal                        *int
}

type ArcadeParentAttachmentWorkerRepository interface {
	Claim(context.Context, string, string, int64) (ArcadeParentAttachmentWorkerClaim, error)
	RootValidation(context.Context, ArcadeParentAttachmentCandidate) (string, error)
	SourceSnapshot(context.Context, string) ([]ArcadeParentSourceSnapshotFile, error)
}

func ValidArcadeParentAttachmentInput(input ArcadeParentAttachmentInput) bool {
	return input.SchemaVersion == 1 && input.AttachmentID != "" && input.ImportItemID != "" &&
		input.ReviewDraftID != "" && input.BaseSourceSnapshotID != "" && input.DependencyMachine != "" &&
		input.ProviderID != "" && input.TargetID != "" && len(input.ContentPolicyDigest) == 64 &&
		input.DATVersionID != "" && input.UploadFileID != ""
}

// ArcadeParentJobQueue is the application port used to resume queued parent
// attachment validations when the process starts.
type ArcadeParentJobQueue interface {
	Queued(context.Context) ([]string, error)
}
