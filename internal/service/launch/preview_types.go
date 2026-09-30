package launch

import (
	"context"
	"errors"
	"time"

	runtimebundle "retrom/internal/runtime/bundle"
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
	SourceSnapshotID, PlatformInstanceID, PlatformName, PlatformKey string
	ProviderID, TargetID, BundleSHA256, CoreID, DeliveryProfile     string
	Title, ContentKind, ValidationStatus, DependencySnapshot        string
	DefaultDOSEntry, DATVersionID                                   *string
}
type PreviewFile struct {
	Role, LogicalName, FileRecord string
	VirtualPath                   *string
	SortOrder                     int
}
type PreviewContent struct {
	FileRecord, LogicalName, Format string
	Files                           []PreviewFile
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
	ActorID, ItemID, SnapshotID, ProviderID, TargetID, State          string
	ContentFileRecord, ContentName, ContentFormat, DependencySnapshot string
	FileRecord, Format                                                string
	HardExpiresAtMS, SizeBytes, MaximumBytes                          int64
	ReadFormats                                                       []string
	Files                                                             []PreviewFile
}
type PreviewCreatePlan struct {
	Request                          ReviewPreviewRequest
	Source                           PreviewSource
	Content                          PreviewContent
	ID, ProfileID                    string
	CredentialHash                   []byte
	NowMS, BootstrapEnd, HardEnd     int64
	RestoreFileRecord, RestoreFormat *string
	RestoreSourceFileRecord          string
	Isolation                        *IsolationTicket
}

// PreviewSessionScope writes a session in the source owner's transaction.
type PreviewSessionScope interface {
	Restore(context.Context, string) (PreviewRestore, bool, error)
	Create(context.Context, PreviewCreatePlan) error
}
type PreviewProvider interface {
	Target(string, string) (runtimebundle.Target, bool)
	BundleSHA256(string, string) (string, bool)
}
type PreviewEnvironment struct {
	Now                   func() time.Time
	NewID                 func() (string, error)
	SignCapability        func(string) (string, []byte, error)
	SignIsolation         func(string) (IsolationTicket, error)
	CopyRestorePayload    func(context.Context, string, string) (string, error)
	DiscardPreviewPayload func(context.Context, string) error
}
