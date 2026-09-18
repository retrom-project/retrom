package payloadrelease

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	model "retrom/internal/model/payloadrelease"
)

type expirationMemory struct {
	providers []model.ProviderExpiration
	previews  []model.PreviewExpiration
	released  []model.ProviderExpiration
	expired   []model.PreviewExpiry
	staged    []string
	fail      error
}

func (memory *expirationMemory) LoadExpiredProviders(
	_ context.Context, _ int64, _ int,
) ([]model.ProviderExpiration, error) {
	return memory.providers, nil
}

func (memory *expirationMemory) LoadExpiredPreviews(
	_ context.Context, _ int64, _ int,
) ([]model.PreviewExpiration, error) {
	return memory.previews, nil
}

func (memory *expirationMemory) CommitProviderExpiration(
	_ context.Context,
	batch model.ProviderExpirationBatch,
	_ model.GCStager,
) error {
	for _, release := range batch.Releases {
		memory.released = append(memory.released, release.Before)
	}
	memory.staged = append(memory.staged, batch.BlobIDs...)
	if memory.fail != nil {
		return memory.fail
	}
	return nil
}

func (memory *expirationMemory) CommitPreviewExpiration(
	_ context.Context,
	batch model.PreviewExpirationBatch,
	_ model.GCStager,
) error {
	memory.expired = append(memory.expired, batch.Expiries...)
	memory.staged = append(memory.staged, batch.BlobIDs...)
	if memory.fail != nil {
		return memory.fail
	}
	return nil
}

func (memory *expirationMemory) StageInScope(
	_ context.Context, _ model.GCScope, ids []string,
) error {
	memory.staged = append(memory.staged, ids...)
	return memory.fail
}

func TestExpirationServiceOwnsPreviewPolicy(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"CREATED", "ACTIVE", "FINISHED", "EXPIRED", "REVOKED"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			memory := &expirationMemory{previews: []model.PreviewExpiration{{
				ID: "preview", State: state, Version: 1, BootstrapExpiresMS: 5, HardExpiresMS: 10,
				CheckpointBlobID: "checkpoint", RestoreBlobID: "restore",
			}}}
			service := NewExpirations(memory, memory, func() time.Time { return time.UnixMilli(10) })
			count, err := service.PreviewBatch(t.Context())
			if err != nil || count != 1 || len(memory.expired) != 1 || len(memory.staged) != 2 {
				t.Fatalf("expire %s: count=%d writes=%v staged=%v err=%v", state, count, memory.expired, memory.staged, err)
			}
			want := "EXPIRED"
			if state == "REVOKED" {
				want = state
			}
			if memory.expired[0].State != want || memory.expired[0].NowMS != 10 {
				t.Fatalf("expiry target = %+v", memory.expired[0])
			}
		})
	}
}

func TestExpirationRejectsStaleFactsAndOverflowBeforeWriting(t *testing.T) {
	t.Parallel()
	for _, preview := range []model.PreviewExpiration{
		{ID: "active", State: "ACTIVE", Version: 1, BootstrapExpiresMS: 5, HardExpiresMS: 11},
		{ID: "expired", State: "EXPIRED", Version: 1, HardExpiresMS: 10},
		{ID: "overflow", State: "CREATED", Version: math.MaxInt64, HardExpiresMS: 10},
	} {
		t.Run(preview.ID, func(t *testing.T) {
			t.Parallel()
			memory := &expirationMemory{previews: []model.PreviewExpiration{preview}}
			service := NewExpirations(memory, memory, func() time.Time { return time.UnixMilli(10) })
			count, err := service.PreviewBatch(t.Context())
			if !errors.Is(err, model.ErrExpirationSnapshotChanged) || count != 0 || len(memory.expired) != 0 {
				t.Fatalf("invalid preview facts wrote expiry: count=%d writes=%v err=%v", count, memory.expired, err)
			}
		})
	}
}

func TestExpirationReturnsNoSuccessWhenGCStagingFails(t *testing.T) {
	t.Parallel()
	cause := errors.New("stage expired response failed")
	memory := &expirationMemory{providers: []model.ProviderExpiration{{
		ID: "response", BlobID: "payload", State: "RETAINED", ExpiresMS: 10,
	}}, fail: cause}
	service := NewExpirations(memory, memory, func() time.Time { return time.UnixMilli(10) })
	count, err := service.ProviderBatch(t.Context())
	if !errors.Is(err, cause) || count != 0 {
		t.Fatalf("GC failure returned expiry success: count=%d err=%v", count, err)
	}
}
