package launch

import (
	launchmodel "retrom/internal/model/launch"
	launchservice "retrom/internal/service/launch"
)

var (
	ErrBlocked          = launchmodel.ErrBlocked
	ErrCredential       = launchmodel.ErrCredential
	ErrDOSEntryMissing  = launchmodel.ErrDOSEntryMissing
	ErrDOSEntryUnsafe   = launchmodel.ErrDOSEntryUnsafe
	ErrSaveIncompatible = launchmodel.ErrSaveIncompatible
)

type Capabilities = launchmodel.Capabilities

type CreateRequest = launchmodel.CreateRequest

type (
	Created              = launchmodel.Created
	NetplayCreateRequest = launchmodel.NetplayCreateRequest
)

type (
	ContentView  = launchmodel.ContentView
	ExternalView = launchmodel.ExternalView
)

type (
	Interval   = launchmodel.Interval
	PlayEvent  = launchmodel.PlayEvent
	PlayResult = launchmodel.PlayResult
)

type (
	Config                       = launchservice.Config
	MultiDiscTelemetryDimensions = launchmodel.MultiDiscTelemetryDimensions
	BundleFile                   = launchmodel.BundleFile
)

var (
	ErrReviewPreviewUnavailable = launchmodel.ErrReviewPreviewUnavailable
	ErrReviewScreenshotInvalid  = launchmodel.ErrReviewScreenshotInvalid
)

type (
	ReviewPreviewRequest = launchmodel.ReviewPreviewRequest
	ReviewPreviewCreated = launchmodel.ReviewPreviewCreated
	ReviewScreenshot     = launchmodel.ReviewScreenshot
)

type ProjectIndexView = launchmodel.ProjectIndexView

var ErrProjectIndexUnavailable = launchmodel.ErrProjectIndexUnavailable

// ProviderAsset identifies one immutable asset declared by the active Target.
type ProviderAsset = launchmodel.ProviderAsset
