package payloadrelease

import (
	"context"
	"fmt"
	"math"
	"time"

	model "retrom/internal/model/payloadrelease"
)

const expirationBatchSize = 200

type Expirations struct {
	repository model.ExpirationRepository
	gc         model.GCStager
	now        func() time.Time
}

func NewExpirations(repository model.ExpirationRepository, gc model.GCStager, now func() time.Time) *Expirations {
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
	now := service.now().UnixMilli()
	responses, err := service.repository.LoadExpiredProviders(ctx, now, expirationBatchSize)
	if err != nil {
		return 0, fmt.Errorf("commit provider expiration: read: %w", err)
	}
	var releases []model.ProviderExpirationRelease
	var blobs []string
	for _, before := range responses {
		if before.ID == "" || before.State != "RETAINED" || before.BlobID == "" ||
			before.Running || before.ExpiresMS > now || before.CacheCount < 0 {
			return 0, fmt.Errorf("commit provider expiration: %w",
				model.ErrExpirationSnapshotChanged)
		}
		releases = append(releases, model.ProviderExpirationRelease{Before: before, NowMS: now})
		blobs = append(blobs, before.BlobID)
	}
	batch := model.ProviderExpirationBatch{
		Releases: releases, BlobIDs: blobs,
	}
	if err := service.repository.CommitProviderExpiration(
		ctx, batch, service.gc,
	); err != nil {
		return 0, fmt.Errorf("commit provider expiration: %w", err)
	}
	return len(responses), nil
}

func (service *Expirations) PreviewBatch(ctx context.Context) (int, error) {
	now := service.now().UnixMilli()
	previews, err := service.repository.LoadExpiredPreviews(ctx, now, expirationBatchSize)
	if err != nil {
		return 0, fmt.Errorf("commit preview expiration: read: %w", err)
	}
	var expiries []model.PreviewExpiry
	var allBlobs []string
	for _, before := range previews {
		if !previewDue(before, now) || before.ID == "" ||
			before.Version < 1 || before.Version == math.MaxInt64 {
			return 0, fmt.Errorf("commit preview expiration: %w",
				model.ErrExpirationSnapshotChanged)
		}
		state := "EXPIRED"
		if before.State == "REVOKED" {
			state = "REVOKED"
		}
		expiries = append(expiries, model.PreviewExpiry{
			Before: before, State: state, NowMS: now,
		})
		allBlobs = append(allBlobs, before.CheckpointBlobID, before.RestoreBlobID)
	}
	batch := model.PreviewExpirationBatch{
		Expiries: expiries, BlobIDs: allBlobs,
	}
	if err := service.repository.CommitPreviewExpiration(
		ctx, batch, service.gc,
	); err != nil {
		return 0, fmt.Errorf("commit preview expiration: %w", err)
	}
	return len(previews), nil
}

func previewDue(before model.PreviewExpiration, now int64) bool {
	due := before.State == "CREATED" && before.BootstrapExpiresMS <= now ||
		before.HardExpiresMS <= now || before.State == "REVOKED"
	remaining := before.State != "EXPIRED" && before.State != "REVOKED" ||
		before.CheckpointBlobID != "" || before.RestoreBlobID != ""
	return due && remaining
}
