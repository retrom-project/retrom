package libraryimport

import (
	"context"
	"errors"
	"testing"

	application "retrom/internal/model/libraryimport"
	"retrom/internal/model/tagging"
)

const (
	testReviewItemID  = "01900000-0000-7000-8000-000000000001"
	testReviewDraftID = "01900000-0000-7000-8000-000000000002"
)

type reviewDraftRepositoryStub struct {
	snapshot  application.ReviewDraftPatchSnapshot
	query     application.ReviewDraftPatchQuery
	plan      application.ReviewDraftWritePlan
	result    DraftResult
	loadErr   error
	commitErr error
}

func (stub *reviewDraftRepositoryStub) LoadPatchSnapshot(
	_ context.Context, query application.ReviewDraftPatchQuery,
) (application.ReviewDraftPatchSnapshot, error) {
	stub.query = query
	if stub.loadErr != nil {
		return application.ReviewDraftPatchSnapshot{}, stub.loadErr
	}
	return stub.snapshot, nil
}

func (stub *reviewDraftRepositoryStub) CommitPatch(
	_ context.Context, plan application.ReviewDraftWritePlan,
) (application.DraftResult, error) {
	stub.plan = plan
	if stub.commitErr != nil {
		return application.DraftResult{}, stub.commitErr
	}
	return stub.result, nil
}

type reviewDraftValidationStub struct{}

func (reviewDraftValidationStub) Resolve(
	context.Context, ReviewDraftValidationRequest,
) (application.ReviewValidationPlan, error) {
	return application.ReviewValidationPlan{}, nil
}

func (reviewDraftValidationStub) SelectScummVM(
	context.Context, ReviewDraftScummVMRequest,
) (application.ReviewValidationPlan, error) {
	return application.ReviewValidationPlan{}, nil
}

func newReviewDraftTestService(repository *reviewDraftRepositoryStub) *ReviewDrafts {
	return NewReviewDrafts(repository, ReviewDraftsOptions{Validation: reviewDraftValidationStub{}})
}

func reviewDraftSnapshot() application.ReviewDraftPatchSnapshot {
	return application.ReviewDraftPatchSnapshot{
		ItemID: testReviewItemID, DraftID: testReviewDraftID, Metadata: map[string]any{},
		BeforeTags: []tagging.Reference{}, ActiveTags: []tagging.Reference{},
	}
}

func TestReviewDraftsRejectsEmptyPatchBeforeRepository(t *testing.T) {
	t.Parallel()
	repository := &reviewDraftRepositoryStub{}
	service := newReviewDraftTestService(repository)
	if _, err := service.Patch(t.Context(), "item", 1, DraftPatch{TagIDs: nil}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
	if repository.query.ItemID != "" {
		t.Fatal("repository called for invalid patch")
	}
}

func TestReviewDraftsForwardsTypedCommandAndActor(t *testing.T) {
	t.Parallel()
	repository := &reviewDraftRepositoryStub{
		snapshot: reviewDraftSnapshot(),
		result:   DraftResult{ItemID: testReviewItemID, Version: 2},
	}
	service := newReviewDraftTestService(repository)
	patch := DraftPatch{Metadata: &MetadataPatch{}, TagIDs: []string{}}
	result, err := service.Patch(t.Context(), testReviewItemID, 1, patch)
	if err != nil {
		t.Fatal(err)
	}
	if result.ItemID != testReviewItemID || repository.query.ItemID != testReviewItemID ||
		repository.query.ExpectedVersion != 1 || repository.plan.Metadata == nil {
		t.Fatalf("plan/result = %#v %#v", repository.plan, result)
	}
	if repository.plan.ActorKind != "SYSTEM" || repository.plan.ActorLabel == nil || *repository.plan.ActorLabel != "release-setup" {
		t.Fatalf("actor = %#v", repository.plan)
	}
}

func TestReviewDraftsPreservesRepositoryError(t *testing.T) {
	t.Parallel()
	cause := errors.New("draft storage unavailable")
	repository := &reviewDraftRepositoryStub{loadErr: cause}
	service := newReviewDraftTestService(repository)
	_, err := service.Patch(t.Context(), testReviewItemID, 1, DraftPatch{Metadata: &MetadataPatch{}, TagIDs: []string{}})
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want cause", err)
	}
}
