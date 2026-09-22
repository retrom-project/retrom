package launch

import application "retrom/internal/service/launch"

var (
	ErrBlocked          = application.ErrBlocked
	ErrCredential       = application.ErrCredential
	ErrDOSEntryMissing  = application.ErrDOSEntryMissing
	ErrDOSEntryUnsafe   = application.ErrDOSEntryUnsafe
	ErrSaveIncompatible = application.ErrSaveIncompatible
)

type Capabilities = application.Capabilities

type CreateRequest = application.CreateRequest

type (
	Created = application.Created
)

type (
	ContentView  = application.ContentView
	ExternalView = application.ExternalView
)

type (
	Interval   = application.Interval
	PlayEvent  = application.PlayEvent
	PlayResult = application.PlayResult
)

type (
	Config                       = application.Config
	MultiDiscTelemetryDimensions = application.MultiDiscTelemetryDimensions
	BundleFile                   = application.BundleFile
)

var (
	ErrReviewPreviewUnavailable = application.ErrReviewPreviewUnavailable
	ErrReviewScreenshotInvalid  = application.ErrReviewScreenshotInvalid
)

type (
	ReviewPreviewRequest = application.ReviewPreviewRequest
	ReviewPreviewCreated = application.ReviewPreviewCreated
	ReviewScreenshot     = application.ReviewScreenshot
)

type ProjectIndexView = application.ProjectIndexView

var ErrProjectIndexUnavailable = application.ErrProjectIndexUnavailable

// ProviderAsset identifies one immutable asset declared by the active Target.
type ProviderAsset = application.ProviderAsset
