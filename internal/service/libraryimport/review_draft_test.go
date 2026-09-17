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
	result    application.DraftResult
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

func (reviewDraftValidationStub) ResolveSelected(
	context.Context, ReviewDraftSelectedValidationRequest,
) (application.ReviewValidationPlan, error) {
	return application.ReviewValidationPlan{}, nil
}

func (reviewDraftValidationStub) SelectScummVM(
	context.Context, ReviewDraftScummVMRequest,
) (application.ReviewValidationPlan, error) {
	return application.ReviewValidationPlan{}, nil
}

type reviewDraftPlanValidationStub struct {
	plan         application.ReviewValidationPlan
	selectedPlan application.ReviewValidationPlan
}

func (stub reviewDraftPlanValidationStub) Resolve(
	context.Context, ReviewDraftValidationRequest,
) (application.ReviewValidationPlan, error) {
	return stub.plan, nil
}

func (stub reviewDraftPlanValidationStub) ResolveSelected(
	context.Context, ReviewDraftSelectedValidationRequest,
) (application.ReviewValidationPlan, error) {
	return stub.selectedPlan, nil
}

func (stub reviewDraftPlanValidationStub) SelectScummVM(
	context.Context, ReviewDraftScummVMRequest,
) (application.ReviewValidationPlan, error) {
	return stub.plan, nil
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
	if _, err := service.Patch(t.Context(), "item", 1, application.DraftPatch{TagIDs: nil}); !errors.Is(err, application.ErrInvalid) {
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
		result:   application.DraftResult{ItemID: testReviewItemID, Version: 2},
	}
	service := newReviewDraftTestService(repository)
	patch := application.DraftPatch{Metadata: &application.MetadataPatch{}, TagIDs: []string{}}
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
	_, err := service.Patch(t.Context(), testReviewItemID, 1, application.DraftPatch{Metadata: &application.MetadataPatch{}, TagIDs: []string{}})
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want cause", err)
	}
}

func TestReviewDraftsRejectsExplicitRPGValidationSelection(t *testing.T) {
	t.Parallel()
	selected := "rpg-validation"
	snapshot := reviewDraftSnapshot()
	snapshot.IsRPG = true
	service := &ReviewDrafts{validation: reviewDraftValidationStub{}}
	_, err := service.resolveValidationPlan(t.Context(), testReviewItemID, "target", nil,
		application.DraftPatch{SelectedValidationID: &selected}, snapshot)
	if !errors.Is(err, application.ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
}

func TestReviewDraftsKeepsScummVMCandidatePlanWithCurrentExplicitSelection(t *testing.T) {
	t.Parallel()
	selected := "validation-a"
	created := &application.ReviewValidationRefreshCreate{ID: "validation-b"}
	copied := &application.ReviewValidationRefreshFileCopy{ValidationID: "validation-b"}
	validation := reviewDraftPlanValidationStub{plan: application.ReviewValidationPlan{
		SelectedValidationID: "validation-b", Create: created, Copy: copied,
	}}
	snapshot := reviewDraftSnapshot()
	snapshot.ValidationID = selected
	service := &ReviewDrafts{validation: validation}
	candidate := "candidate-b"
	plan, err := service.resolveValidationPlan(t.Context(), testReviewItemID, "target", nil, application.DraftPatch{
		ScummVMCandidateID: &candidate, SelectedValidationID: &selected,
	}, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if plan.SelectedValidationID != "validation-b" || plan.Create != created || plan.Copy != copied {
		t.Fatalf("candidate plan was overwritten: %#v", plan)
	}
}

func TestReviewDraftsKeepsExplicitNonRPGSelection(t *testing.T) {
	t.Parallel()
	selected := "validation-explicit"
	validation := reviewDraftPlanValidationStub{selectedPlan: application.ReviewValidationPlan{
		SelectedValidationID: selected,
	}}
	validation.plan = application.ReviewValidationPlan{
		SelectedValidationID: "resolver-selection",
		Create:               &application.ReviewValidationRefreshCreate{ID: "new-validation"},
		Copy:                 &application.ReviewValidationRefreshFileCopy{ValidationID: "new-validation"},
	}
	service := &ReviewDrafts{validation: validation}
	plan, err := service.resolveValidationPlan(t.Context(), testReviewItemID, "target", nil, application.DraftPatch{
		SelectedValidationID: &selected,
	}, reviewDraftSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if plan.SelectedValidationID != selected || plan.Create != nil || plan.Copy != nil {
		t.Fatalf("explicit selection was not preserved: %#v", plan)
	}
}
