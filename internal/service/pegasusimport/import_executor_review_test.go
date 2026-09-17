package pegasusimport

import (
	"errors"
	"strings"
	"testing"

	model "retrom/internal/model/pegasusimport"

	library "retrom/internal/model/libraryimport"
)

func TestImportExecutorRetainsBoundReviewIdentityWhenHandoffFails(t *testing.T) {
	t.Parallel()
	failure := errors.New("review metadata unavailable")
	source := &preparationSource{found: true, result: library.ServerImportResult{
		Created: library.ServerCreated{ImportJobID: "library-job"},
		Items:   []library.ServerImportItem{{ItemID: "library-item", State: "REVIEW_PENDING"}},
	}}
	transitions := &preparationOutcomes{}
	handoff := &preparationOutcomes{failure: failure}
	fixture, executor := newImportExecutorFixture()
	executor.dependencies.Reviews = NewReviewPreparation(source, transitions, handoff)
	if err := executor.Process(t.Context(), model.Work{}, fixture.items[0]); err != nil {
		t.Fatal(err)
	}
	if !transitions.resumed || handoff.handoffs != 1 || len(fixture.outcomes) != 1 {
		t.Fatalf("bound review did not reach failing handoff: resumed=%v handoffs=%d outcomes=%+v", transitions.resumed, handoff.handoffs, fixture.outcomes)
	}
	outcome := fixture.outcomes[0]
	if outcome.State != "COMMIT_FAILED" || outcome.Failure == nil {
		t.Fatalf("failure outcome=%+v", outcome)
	}
	details := outcome.Failure
	if details.LibraryImportJobID == nil || details.LibraryImportItemID == nil ||
		*details.LibraryImportJobID != "library-job" || *details.LibraryImportItemID != "library-item" {
		t.Errorf("handoff failure lost permanent identity: %+v", details)
	}
	if !strings.Contains(details.TechnicalDetail, failure.Error()) {
		t.Errorf("handoff failure lost original cause: %+v", details)
	}
}
