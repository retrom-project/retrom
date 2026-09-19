package libraryimport

import (
	"context"
	"errors"
	"testing"
	"time"

	application "retrom/internal/model/libraryimport"
)

type reviewPreviewValidationRepositoryStub struct {
	draft     application.ReviewPreviewValidationDraft
	itemID    string
	plan      application.ReviewPreviewValidationPlan
	draftErr  error
	commitErr error
}

func (repository *reviewPreviewValidationRepositoryStub) Draft(
	_ context.Context, itemID string,
) (application.ReviewPreviewValidationDraft, error) {
	repository.itemID = itemID
	if repository.draftErr != nil {
		return application.ReviewPreviewValidationDraft{}, repository.draftErr
	}
	return repository.draft, nil
}

func (repository *reviewPreviewValidationRepositoryStub) Commit(
	_ context.Context, plan application.ReviewPreviewValidationPlan,
) error {
	repository.plan = plan
	return repository.commitErr
}

type reviewPreviewValidationStub struct{}

func (reviewPreviewValidationStub) Resolve(
	context.Context, ReviewDraftValidationRequest,
) (application.ReviewValidationPlan, error) {
	return application.ReviewValidationPlan{SelectedValidationID: "validation"}, nil
}

func (reviewPreviewValidationStub) ResolveSelected(
	context.Context, ReviewDraftSelectedValidationRequest,
) (application.ReviewValidationPlan, error) {
	return application.ReviewValidationPlan{}, nil
}

func (reviewPreviewValidationStub) SelectScummVM(
	context.Context, ReviewDraftScummVMRequest,
) (application.ReviewValidationPlan, error) {
	return application.ReviewValidationPlan{}, nil
}

func TestReviewPreviewValidationsRejectsMissingItemBeforeStorage(t *testing.T) {
	t.Parallel()
	repository := &reviewPreviewValidationRepositoryStub{}
	service := NewReviewPreviewValidations(repository, reviewPreviewValidationStub{}, func() time.Time {
		return time.UnixMilli(7)
	})
	if err := service.Refresh(t.Context(), " "); !errors.Is(err, application.ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
	if repository.itemID != "" {
		t.Fatalf("repository called for invalid item: %#v", repository)
	}
}

func TestReviewPreviewValidationsPassesClockAndWrapsRepositoryError(t *testing.T) {
	t.Parallel()
	want := errors.New("storage failed")
	repository := &reviewPreviewValidationRepositoryStub{
		draft:     application.ReviewPreviewValidationDraft{TargetID: "target", Version: 3},
		commitErr: want,
	}
	service := NewReviewPreviewValidations(repository, reviewPreviewValidationStub{}, func() time.Time {
		return time.UnixMilli(42)
	})
	if err := service.Refresh(t.Context(), "item"); !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapped storage error", err)
	}
	if repository.itemID != "item" || repository.plan.NowMS != 42 || repository.plan.ExpectedVersion != 3 {
		t.Fatalf("repository call = %#v", repository)
	}
}
