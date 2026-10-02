package libraryimport

import (
	"context"
	"errors"

	contentcapability "retrom/internal/content/capability"
	"retrom/internal/service/cleanupjobs"

	"retrom/internal/service/importprogress"
	"retrom/internal/service/tagging"
)

var ErrDuplicateContent = errors.New("DUPLICATE_GAME_CONFIRMATION_REQUIRED")

type DuplicateConflict struct {
	ContentIdentityDigest string          `json:"contentIdentityDigest"`
	Games                 []DuplicateGame `json:"games"`
}

func (conflict *DuplicateConflict) Error() string { return ErrDuplicateContent.Error() }
func (conflict *DuplicateConflict) Unwrap() error { return ErrDuplicateContent }

type ReviewApproved struct {
	GameID string `json:"gameId"`
	Status string `json:"status"`
}

type ReviewApprovalDecision struct {
	Reason              *string
	DuplicatePolicy     string
	AcknowledgedGameIDs []string
	SourceKind          string
	ExternalAssets      []ApprovalExternalAsset
}

type ReviewApprovalRequest struct {
	ItemID          string
	ExpectedVersion int64
	Decision        ReviewApprovalDecision
	Bulk            *BulkPublicationIntent
}

type BulkPublicationIntent struct {
	BulkID, JobID, WorkerID, SourceSnapshotID string
}

type ApprovalExternalAsset struct {
	Kind, FileRecord, MediaType string
	WidthPX, HeightPX           *int64
}

type ReviewApprovalHead struct {
	DraftID, State, ImportID, PlatformID, PlatformInstanceID              string
	ValidationStatus, MetadataJSON                                        string
	SourceSnapshotID, SourceManifestJSON, SourceManifestDigest            string
	ContentKind, CoreID, ProviderID, TargetID, DependencyJSON             string
	Policy                                                                contentcapability.Policy
	DraftVersion, ParentVersion                                           int64
	DATID, ValidationDOS, DraftDOS, CandidateID                           *string
	CoverID, UploadedCoverID, UploadedVideoID, BackgroundID, ScreenshotID *string
	SourceBusy                                                            bool
	Progress                                                              importprogress.Snapshot
	RuntimeFiles                                                          []PreparedValidationFile
}

type ApprovalOrigin struct {
	Kind   string
	Assets []ApprovalExternalAsset
}

type ReviewApprovalRepository interface {
	PendingPublications(context.Context) ([]ReviewApprovalRequest, error)
	WithApproval(context.Context, func(ReviewApprovalScope) error) error
}

type ReviewApprovalScope struct {
	Publications PublicationRecords
	Payload      cleanupjobs.ItemSchedulingScope
	Reader       ReviewApprovalReader
	Media        ApprovalMediaReader
	Profiles     ReviewProfileReader
	Dependencies ApprovalDependencyScope
	Duplicates   ContentDuplicateReader
	Tags         tagging.WriteScope
	Games        ApprovalGameWriter
	Variants     ApprovalVariantWriter
	Decisions    ApprovalDecisionWriter
	Bulk         BulkPublicationWriter
}

type ReviewApprovalReader interface {
	Head(context.Context, string) (ReviewApprovalHead, bool, error)
	Origin(context.Context, string) (ApprovalOrigin, bool, error)
}

type ApprovalMediaReader interface {
	Candidate(context.Context, string, string) (ApprovalExternalAsset, bool, error)
	UploadedAsset(context.Context, string, string, string) (ApprovalExternalAsset, bool, error)
	Screenshots(context.Context, string) ([]string, error)
	RuntimeScreenshot(context.Context, string, string) (ApprovalExternalAsset, bool, error)
}
