package cleanupjobs_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	jobs "retrom/internal/service/cleanupjobs"
	previewservice "retrom/internal/service/libraryimport/previewretention"
	provider "retrom/internal/service/metadatascrape/providerretention"
)

type expirationMemory struct {
	providers []jobs.ProviderExpiration
	previews  []jobs.PreviewExpiration
	released  []jobs.ProviderExpiration
	expired   []jobs.PreviewExpiry
	staged    []string
	fail      error
}

func (memory *expirationMemory) WithProviderExpiration(_ context.Context,
	run func(jobs.ProviderExpirationScope) error,
) error {
	return run(jobs.ProviderExpirationScope{Read: memory, Write: memory})
}

func (memory *expirationMemory) WithPreviewExpiration(_ context.Context,
	run func(jobs.PreviewExpirationScope) error,
) error {
	return run(jobs.PreviewExpirationScope{Read: memory, Write: memory})
}

func (memory *expirationMemory) Providers(context.Context, int64, int) ([]jobs.ProviderExpiration, error) {
	return memory.providers, nil
}

func (memory *expirationMemory) Previews(context.Context, int64, int) ([]jobs.PreviewExpiration, error) {
	return memory.previews, nil
}

func (memory *expirationMemory) ReleaseProvider(_ context.Context, before jobs.ProviderExpiration, _ int64) error {
	memory.released = append(memory.released, before)
	return nil
}

func (memory *expirationMemory) ExpirePreview(_ context.Context, change jobs.PreviewExpiry) error {
	memory.expired = append(memory.expired, change)
	return nil
}

func (memory *expirationMemory) StageInScope(_ context.Context, _ jobs.DeletionScope, ids []string) error {
	memory.staged = append(memory.staged, ids...)
	return memory.fail
}

func TestExpirationServiceOwnsPreviewPolicy(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"CREATED", "ACTIVE", "FINISHED", "EXPIRED", "REVOKED"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			memory := &expirationMemory{previews: []jobs.PreviewExpiration{{
				ID: "preview", State: state, Version: 1, BootstrapExpiresMS: 5, HardExpiresMS: 10,
				CheckpointFileRecord: "checkpoint", RestoreFileRecord: "restore",
			}}}
			service := previewservice.New(memory, memory, func() time.Time { return time.UnixMilli(10) })
			count, err := service.PreviewBatch(t.Context())
			if err != nil || count != 1 || len(memory.expired) != 1 || len(memory.staged) != 0 {
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
	for _, preview := range []jobs.PreviewExpiration{
		{ID: "active", State: "ACTIVE", Version: 1, BootstrapExpiresMS: 5, HardExpiresMS: 11},
		{ID: "expired", State: "EXPIRED", Version: 1, HardExpiresMS: 10},
		{ID: "overflow", State: "CREATED", Version: math.MaxInt64, HardExpiresMS: 10},
	} {
		t.Run(preview.ID, func(t *testing.T) {
			t.Parallel()
			memory := &expirationMemory{previews: []jobs.PreviewExpiration{preview}}
			service := previewservice.New(memory, memory, func() time.Time { return time.UnixMilli(10) })
			count, err := service.PreviewBatch(t.Context())
			if !errors.Is(err, jobs.ErrExpirationSnapshotChanged) || count != 0 || len(memory.expired) != 0 {
				t.Fatalf("invalid preview facts wrote expiry: count=%d writes=%v err=%v", count, memory.expired, err)
			}
		})
	}
}

func TestExpirationReturnsNoSuccessWhenGCStagingFails(t *testing.T) {
	t.Parallel()
	cause := errors.New("stage expired response failed")
	memory := &expirationMemory{providers: []jobs.ProviderExpiration{{
		ID: "response", FileRecord: "payload", State: "RETAINED", ExpiresMS: 10,
	}}, fail: cause}
	service := provider.New(memory, memory, func() time.Time { return time.UnixMilli(10) })
	count, err := service.ProviderBatch(t.Context())
	if !errors.Is(err, cause) || count != 0 {
		t.Fatalf("file deletion failure returned expiry success: count=%d err=%v", count, err)
	}
}
