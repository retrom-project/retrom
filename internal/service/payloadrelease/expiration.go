package payloadrelease

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"
)

var ErrExpirationSnapshotChanged = errors.New("PAYLOAD_EXPIRATION_SNAPSHOT_CHANGED")

const expirationBatchSize = 200

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

type ExpirationRepository interface {
	WithExpiration(context.Context, func(ExpirationScope) error) error
}

type GCStager interface {
	StageInScope(context.Context, GCScope, []string) error
}

type Expirations struct {
	repository ExpirationRepository
	gc         GCStager
	now        func() time.Time
}

func NewExpirations(repository ExpirationRepository, gc GCStager, now func() time.Time) *Expirations {
	if now == nil {
		now = time.Now
	}
	return &Expirations{repository: repository, gc: gc, now: now}
}

func (service *Expirations) Providers(ctx context.Context) error {
	for {
		count, err := service.ProviderBatch(ctx)
		if err != nil || count < expirationBatchSize {
			return err
		}
	}
}

func (service *Expirations) Previews(ctx context.Context) error {
	for {
		count, err := service.PreviewBatch(ctx)
		if err != nil || count < expirationBatchSize {
			return err
		}
	}
}

func (service *Expirations) ProviderBatch(ctx context.Context) (int, error) {
	count := 0
	err := service.repository.WithExpiration(ctx, func(scope ExpirationScope) error {
		now := service.now().UnixMilli()
		responses, err := scope.Read.Providers(ctx, now, expirationBatchSize)
		if err != nil {
			return fmt.Errorf("read expired provider payloads: %w", err)
		}
		var blobs []string
		for _, before := range responses {
			if before.ID == "" || before.State != "RETAINED" || before.BlobID == "" ||
				before.Running || before.ExpiresMS > now || before.CacheCount < 0 {
				return ErrExpirationSnapshotChanged
			}
			if err := scope.Write.ReleaseProvider(ctx, before, now); err != nil {
				return fmt.Errorf("release expired provider payload: %w", err)
			}
			blobs = append(blobs, before.BlobID)
		}
		if err := service.gc.StageInScope(ctx, scope.GC, blobs); err != nil {
			return fmt.Errorf("stage expired provider payloads: %w", err)
		}
		count = len(responses)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("commit provider expiration: %w", err)
	}
	return count, nil
}

func (service *Expirations) PreviewBatch(ctx context.Context) (int, error) {
	count := 0
	err := service.repository.WithExpiration(ctx, func(scope ExpirationScope) error {
		now := service.now().UnixMilli()
		previews, err := scope.Read.Previews(ctx, now, expirationBatchSize)
		if err != nil {
			return fmt.Errorf("read expired previews: %w", err)
		}
		for _, before := range previews {
			if !previewDue(before, now) || before.ID == "" || before.Version < 1 || before.Version == math.MaxInt64 {
				return ErrExpirationSnapshotChanged
			}
			state := "EXPIRED"
			if before.State == "REVOKED" {
				state = "REVOKED"
			}
			if err := scope.Write.ExpirePreview(ctx, PreviewExpiry{Before: before, State: state, NowMS: now}); err != nil {
				return fmt.Errorf("expire review preview: %w", err)
			}
			blobs := []string{before.CheckpointBlobID, before.RestoreBlobID}
			if err := service.gc.StageInScope(ctx, scope.GC, blobs); err != nil {
				return fmt.Errorf("stage expired preview payloads: %w", err)
			}
		}
		count = len(previews)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("commit preview expiration: %w", err)
	}
	return count, nil
}

func previewDue(before PreviewExpiration, now int64) bool {
	due := before.State == "CREATED" && before.BootstrapExpiresMS <= now ||
		before.HardExpiresMS <= now || before.State == "REVOKED"
	remaining := before.State != "EXPIRED" && before.State != "REVOKED" ||
		before.CheckpointBlobID != "" || before.RestoreBlobID != ""
	return due && remaining
}
