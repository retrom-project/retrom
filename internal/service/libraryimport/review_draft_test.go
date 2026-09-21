package libraryimport

import (
	"context"
	"errors"
	"testing"
)

type reviewDraftRepositoryStub struct {
	request ReviewDraftPatchRequest
	result  DraftResult
	err     error
}

func (stub *reviewDraftRepositoryStub) Patch(
	_ context.Context, request ReviewDraftPatchRequest,
) (DraftResult, error) {
	stub.request = request
	return stub.result, stub.err
}

func TestReviewDraftsRejectsEmptyPatchBeforeRepository(t *testing.T) {
	t.Parallel()
	repository := &reviewDraftRepositoryStub{}
	service := NewReviewDrafts(repository)
	if _, err := service.Patch(t.Context(), "item", 1, DraftPatch{TagIDs: nil}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
	if repository.request.ItemID != "" {
		t.Fatal("repository called for invalid patch")
	}
}

func TestReviewDraftsForwardsTypedCommandAndActor(t *testing.T) {
	t.Parallel()
	repository := &reviewDraftRepositoryStub{result: DraftResult{ItemID: "item", Version: 2}}
	service := NewReviewDrafts(repository)
	patch := DraftPatch{Metadata: &MetadataPatch{}, TagIDs: []string{}}
	result, err := service.Patch(t.Context(), "item", 1, patch)
	if err != nil {
		t.Fatal(err)
	}
	if result.ItemID != "item" || repository.request.ItemID != "item" ||
		repository.request.ExpectedVersion != 1 || repository.request.Patch.Metadata == nil {
		t.Fatalf("request/result = %#v %#v", repository.request, result)
	}
	if repository.request.Actor.Kind != "SYSTEM" || repository.request.Actor.Label != "release-setup" {
		t.Fatalf("actor = %#v", repository.request.Actor)
	}
}

func TestReviewDraftsPreservesRepositoryError(t *testing.T) {
	t.Parallel()
	cause := errors.New("draft storage unavailable")
	repository := &reviewDraftRepositoryStub{err: cause}
	service := NewReviewDrafts(repository)
	_, err := service.Patch(t.Context(), "item", 1, DraftPatch{Metadata: &MetadataPatch{}, TagIDs: []string{}})
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want cause", err)
	}
}
