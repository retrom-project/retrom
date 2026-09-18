package payloadrelease

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

type ExpirationReader interface {
	Providers(context.Context, int64, int) ([]ProviderExpiration, error)
	Previews(context.Context, int64, int) ([]PreviewExpiration, error)
}

type ExpirationWriter interface {
	ReleaseProvider(context.Context, ProviderExpiration, int64) error
	ExpirePreview(context.Context, PreviewExpiry) error
}

type ExpirationScope struct {
	Read  ExpirationReader
	Write ExpirationWriter
	GC    GCScope
}

type ProviderExpirationBatch struct {
	Releases []ProviderExpirationRelease
	BlobIDs  []string
}

type ProviderExpirationRelease struct {
	Before ProviderExpiration
	NowMS  int64
}

type PreviewExpirationBatch struct {
	Expiries []PreviewExpiry
	BlobIDs  []string
}

type ExpirationRepository interface {
	LoadExpiredProviders(context.Context, int64, int) ([]ProviderExpiration, error)
	LoadExpiredPreviews(context.Context, int64, int) ([]PreviewExpiration, error)
	CommitProviderExpiration(context.Context, ProviderExpirationBatch, GCStager) error
	CommitPreviewExpiration(context.Context, PreviewExpirationBatch, GCStager) error
}

type GCStager interface {
	StageInScope(context.Context, GCScope, []string) error
}
