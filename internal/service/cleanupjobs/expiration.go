package cleanupjobs

import (
	"context"
	"errors"
)

var ErrExpirationSnapshotChanged = errors.New("PAYLOAD_EXPIRATION_SNAPSHOT_CHANGED")

type ProviderExpiration struct {
	ID, BlobID, State     string
	ExpiresMS, CacheCount int64
	Running               bool
}

type PreviewExpiration struct {
	ID, State, CheckpointBlobID, RestoreBlobID string
	Version, BootstrapExpiresMS, HardExpiresMS int64
	FinishedMS                                 *int64
}

type PreviewExpiry struct {
	Before PreviewExpiration
	State  string
	NowMS  int64
}

type ProviderExpirationReader interface {
	Providers(context.Context, int64, int) ([]ProviderExpiration, error)
}
type ProviderExpirationWriter interface {
	ReleaseProvider(context.Context, ProviderExpiration, int64) error
}
type PreviewExpirationReader interface {
	Previews(context.Context, int64, int) ([]PreviewExpiration, error)
}
type PreviewExpirationWriter interface {
	ExpirePreview(context.Context, PreviewExpiry) error
}
type ProviderExpirationScope struct {
	Read          ProviderExpirationReader
	Write         ProviderExpirationWriter
	DeletionQueue DeletionScope
}
type PreviewExpirationScope struct {
	Read          PreviewExpirationReader
	Write         PreviewExpirationWriter
	DeletionQueue DeletionScope
}
type ProviderExpirationRepository interface {
	WithProviderExpiration(context.Context, func(ProviderExpirationScope) error) error
}
type PreviewExpirationRepository interface {
	WithPreviewExpiration(context.Context, func(PreviewExpirationScope) error) error
}

type DeletionStager interface {
	StageInScope(context.Context, DeletionScope, []string) error
}
