package tagging

import (
	"errors"
	"testing"
)

func TestBuildReplacementPlanComputesOnlyTheRelationDelta(t *testing.T) {
	t.Parallel()
	owner := Owner{Kind: OwnerReviewDraft, ID: "01900000-0000-7000-8000-000000000001"}
	before := []Reference{{TagID: "01900000-0000-7000-8000-000000000002", Name: "old"}}
	after := []Reference{{TagID: "01900000-0000-7000-8000-000000000003", Name: "new"}}

	plan, err := BuildReplacementPlan(owner, before, after, "01900000-0000-7000-8000-000000000004", 42)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Changed || len(plan.Added) != 1 || len(plan.Removed) != 1 || plan.NowMS != 42 {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.Added[0] != after[0] || plan.Removed[0] != before[0] {
		t.Fatalf("delta = %#v %#v", plan.Added, plan.Removed)
	}
}

func TestBuildReplacementPlanPreservesEmptyAfterAsAnArray(t *testing.T) {
	t.Parallel()
	owner := Owner{Kind: OwnerEmulationStationCollection, ID: "01900000-0000-7000-8000-000000000001"}
	after := []Reference{}

	plan, err := BuildReplacementPlan(owner, []Reference{{TagID: "01900000-0000-7000-8000-000000000002"}}, after, "01900000-0000-7000-8000-000000000004", 42)
	if err != nil {
		t.Fatal(err)
	}
	if plan.After == nil {
		t.Fatal("plan.After must remain a non-nil empty slice so JSON persistence emits []")
	}
}

func TestValidateActiveReferenceFactsRejectsMissingTagsWithoutIO(t *testing.T) {
	t.Parallel()
	_, err := ValidateActiveReferenceFacts(
		[]string{"01900000-0000-7000-8000-000000000002"},
		[]Reference{},
	)
	if !errors.Is(err, ErrReferenceInvalid) {
		t.Fatalf("error = %v, want ErrReferenceInvalid", err)
	}
}

func TestLayeringReferenceFactsRequireExactMembership(t *testing.T) {
	t.Parallel()
	a := "01980000-0000-7000-8000-00000000c001"
	b := "01980000-0000-7000-8000-00000000c002"
	c := "01980000-0000-7000-8000-00000000c003"

	// Request {A,B} but facts are {A,C} → must reject even though counts match
	_, err := ValidateActiveReferenceFacts(
		[]string{a, b},
		[]Reference{{TagID: a, Name: "a"}, {TagID: c, Name: "c"}},
	)
	if err == nil {
		t.Fatal("expected error for mismatched members with equal count")
	}
	var refErr *InvalidReferencesError
	if !errors.As(err, &refErr) {
		t.Fatalf("expected InvalidReferencesError, got %T: %v", err, err)
	}
	if len(refErr.IDs) != 1 || refErr.IDs[0] != b {
		t.Fatalf("expected missing=[%s], got %v", b, refErr.IDs)
	}

	// Request {A,B,C} with facts {A,B} → must report missing C
	_, err = ValidateActiveReferenceFacts(
		[]string{a, b, c},
		[]Reference{{TagID: a, Name: "a"}, {TagID: b, Name: "b"}},
	)
	if err == nil {
		t.Fatal("expected error for missing member")
	}

	// Correct: request {A,B} with facts {A,B} → success
	refs, err := ValidateActiveReferenceFacts(
		[]string{a, b},
		[]Reference{{TagID: a, Name: "a"}, {TagID: b, Name: "b"}},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}

	// Correct: empty request, empty facts → success
	refs, err = ValidateActiveReferenceFacts([]string{}, []Reference{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected 0 refs, got %d", len(refs))
	}
}

func TestLayeringReferenceFactsRejectDuplicateFacts(t *testing.T) {
	t.Parallel()
	a := "01980000-0000-7000-8000-00000000c001"
	b := "01980000-0000-7000-8000-00000000c002"

	// Request {A,B} but facts are {A,A,B} → must reject duplicate
	_, err := ValidateActiveReferenceFacts(
		[]string{a, b},
		[]Reference{{TagID: a, Name: "a"}, {TagID: a, Name: "a2"}, {TagID: b, Name: "b"}},
	)
	if err == nil {
		t.Fatal("expected error for duplicate facts")
	}
}

func TestLayeringReferenceFactsRejectExtraUnrequestedFacts(t *testing.T) {
	t.Parallel()
	a := "01980000-0000-7000-8000-00000000c001"

	// Empty request but non-empty facts → must reject
	_, err := ValidateActiveReferenceFacts(
		[]string{},
		[]Reference{{TagID: a, Name: "a"}},
	)
	if err == nil {
		t.Fatal("expected error for unrequested facts")
	}
}
