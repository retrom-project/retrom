package launch

import (
	"context"
	"errors"
	"time"

	"retrom/internal/capability/runtime/runtimebundle"
)

var (
	ErrReviewPreviewUnavailable = errors.New("REVIEW_PREVIEW_UNAVAILABLE")
	ErrSaveIncompatible         = errors.New("LAUNCH_SAVE_INCOMPATIBLE")
)

type ReviewPreviewRequest struct {
	ImportItemID, ActorUserID, IdempotencyKey string
	ClientCapabilities                        Capabilities
	RestoreFromPreviewID                      *string
}
type ReviewPreviewCreated struct {
	PreviewID  string `json:"previewId"`
	PlayURL    string `json:"playUrl"`
	Capability string `json:"-"`
}
type PreviewSource struct {
	SourceSnapshotID, PlatformInstanceID, PlatformName, PlatformKey        string
	ProviderID, TargetID, BundleSHA256, CoreID, DeliveryProfile            string
	Title, ContentKind, ValidationID, ValidationStatus, DependencySnapshot string
	DefaultDOSEntry, SelectedValidationID, DATVersionID                    *string
}
type PreviewFile struct {
	Role, LogicalName, BlobID string
	VirtualPath               *string
	SortOrder                 int
}
type PreviewContent struct {
	BlobID, LogicalName, Format string
	Files                       []PreviewFile
}
type PreviewSnapshot struct {
	Source                       PreviewSource
	SourceFiles, ValidationFiles []PreviewFile
}
type PreviewReceipt struct {
	ID, ImportItemID     string
	RestoreFromPreviewID *string
}
type PreviewRestore struct {
	ActorID, ItemID, SnapshotID, ProviderID, TargetID, State      string
	ContentBlobID, ContentName, ContentFormat, DependencySnapshot string
	BlobID, Format                                                string
	HardExpiresAtMS, SizeBytes, MaximumBytes                      int64
	ReadFormats                                                   []string
	Files                                                         []PreviewFile
}
type PreviewCreatePlan struct {
	Request                      ReviewPreviewRequest
	Source                       PreviewSource
	Content                      PreviewContent
	ID, ProfileID                string
	CredentialHash               []byte
	NowMS, BootstrapEnd, HardEnd int64
	RestoreBlobID, RestoreFormat *string
	Isolation                    *IsolationTicket
}
type PreviewCreationScope interface {
	Replay(context.Context, string, string) (PreviewReceipt, bool, error)
	Current(context.Context, ReviewPreviewRequest) (PreviewSource, string, bool, error)
	Restore(context.Context, string) (PreviewRestore, bool, error)
	Create(context.Context, PreviewCreatePlan) error
}
type PreviewCreationRepository interface {
	Replay(context.Context, string, string) (PreviewReceipt, bool, error)
	Snapshot(context.Context, string) (PreviewSnapshot, bool, error)
	WithCreation(context.Context, func(PreviewCreationScope) error) error
}
type PreviewProvider interface {
	Target(string, string) (runtimebundle.Target, bool)
	BundleSHA256(string, string) (string, bool)
}
type PreviewEnvironment struct {
	Now            func() time.Time
	NewID          func() (string, error)
	SignCapability func(string) (string, []byte, error)
	SignIsolation  func(string) (IsolationTicket, error)
}
