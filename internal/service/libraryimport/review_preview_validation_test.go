package libraryimport

import (
	"context"
	"errors"
	"testing"
	"time"
)

type reviewPreviewValidationRepositoryStub struct {
	itemID string
	nowMS  int64
	err    error
}

func (repository *reviewPreviewValidationRepositoryStub) Refresh(
	_ context.Context, itemID string, nowMS int64,
) error {
	repository.itemID = itemID
	repository.nowMS = nowMS
	return repository.err
}

func TestReviewPreviewValidationsRejectsMissingItemBeforeStorage(t *testing.T) {
	t.Parallel()
	repository := &reviewPreviewValidationRepositoryStub{}
	service := NewReviewPreviewValidations(repository, func() time.Time { return time.UnixMilli(7) })
	if err := service.Refresh(t.Context(), " "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
	if repository.itemID != "" {
		t.Fatalf("repository called for invalid item: %#v", repository)
	}
}

func TestReviewPreviewValidationsPassesClockAndWrapsRepositoryError(t *testing.T) {
	t.Parallel()
	want := errors.New("storage failed")
	repository := &reviewPreviewValidationRepositoryStub{err: want}
	service := NewReviewPreviewValidations(repository, func() time.Time { return time.UnixMilli(42) })
	if err := service.Refresh(t.Context(), "item"); !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapped storage error", err)
	}
	if repository.itemID != "item" || repository.nowMS != 42 {
		t.Fatalf("repository call = %#v", repository)
	}
}
