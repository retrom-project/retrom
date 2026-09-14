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
