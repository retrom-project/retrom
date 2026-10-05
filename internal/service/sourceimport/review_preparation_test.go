package sourceimport

import (
	"context"
	"errors"
	"testing"

	"retrom/internal/content/diagnostic"

	library "retrom/internal/service/libraryimport"
)

type preparationSource struct {
	result  library.ServerImportResult
	found   bool
	creates int
	failure error
}

func (fake *preparationSource) LookupOwnedServerSource(context.Context, library.SourceCreationIntent) (library.ServerImportResult, bool, error) {
	return fake.result, fake.found, nil
}

func (fake *preparationSource) CreateOwnedServerSource(context.Context, library.OwnedServerSourceRequest) (library.ServerImportResult, error) {
	fake.creates++
	return fake.result, fake.failure
}

type preparationOutcomes struct {
	resumed  bool
	outcome  *ItemOutcome
	handoffs int
	failure  error
}

func (fake *preparationOutcomes) Resume(context.Context, ExecutionIdentity, string, string, string) error {
	fake.resumed = true
	return fake.failure
}

func (fake *preparationOutcomes) Finish(_ context.Context, _ ExecutionIdentity, _ string, outcome ItemOutcome) error {
	fake.outcome = &outcome
	return fake.failure
}

func (fake *preparationOutcomes) Complete(context.Context, ReviewHandoffRequest) error {
	fake.handoffs++
	return fake.failure
}

func TestReviewPreparationReplaysPermanentBindingWithoutSourcePaths(t *testing.T) {
	t.Parallel()
	source := &preparationSource{found: true, result: library.ServerImportResult{Created: library.ServerCreated{ImportJobID: "library-job"}, Items: []library.ServerImportItem{{ItemID: "library-item", State: "DISCARDED", ExistingGameID: "game-a", ExistingMatches: []library.ServerDuplicateMatch{{GameID: "game-a"}, {GameID: "game-b"}}}}}}
	outcomes := &preparationOutcomes{}
	service := NewReviewPreparation(source, outcomes, outcomes)
	found, err := service.Resume(t.Context(), Work{ImportID: "import", JobID: "job", WorkerID: "owner", ExecutionNo: 1, Attempt: 1}, ExecutionItem{ID: "item"})
	if err != nil || !found || source.creates != 0 || !outcomes.resumed || outcomes.handoffs != 0 {
		t.Fatalf("replay found=%v err=%v creates=%d outcomes=%+v", found, err, source.creates, outcomes)
	}
	if outcomes.outcome == nil || outcomes.outcome.State != "SKIPPED_EXISTING" || len(outcomes.outcome.ExistingMatches) != 2 {
		t.Fatalf("duplicate replay lost matches: %+v", outcomes.outcome)
	}
}

func TestReviewPreparationRejectsStaleOwnerBeforeMetadataOrOutcome(t *testing.T) {
	t.Parallel()
	source := &preparationSource{found: true, result: library.ServerImportResult{Created: library.ServerCreated{ImportJobID: "job"}, Items: []library.ServerImportItem{{ItemID: "item", State: "REVIEW_PENDING"}}}}
	outcomes := &preparationOutcomes{failure: ErrVersionConflict}
	_, err := NewReviewPreparation(source, outcomes, outcomes).Resume(t.Context(), Work{}, ExecutionItem{})
	if !errors.Is(err, ErrVersionConflict) || outcomes.handoffs != 0 || outcomes.outcome != nil {
		t.Fatalf("stale replay wrote review: %v %+v", err, outcomes)
	}
}

func TestReviewPreparationSeedsOnlyOnePendingReview(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"REVIEW_PENDING", "BLOCKED"} {
		t.Run(state, func(t *testing.T) {
			source := &preparationSource{result: library.ServerImportResult{Created: library.ServerCreated{ImportJobID: "job"}, Items: []library.ServerImportItem{{ItemID: "item", State: state}}}}
			outcomes := &preparationOutcomes{}
			err := NewReviewPreparation(source, outcomes, outcomes).Create(t.Context(), Work{}, ExecutionItem{Files: []ExecutionFile{{Path: "game.zip"}}}, []library.ServerSourceFile{{RelativePath: "game.zip"}})
			if state == "BLOCKED" {
				if !errors.Is(err, ErrInvalid) || outcomes.outcome != nil || outcomes.handoffs != 0 {
					t.Fatalf("invalid library state disguised as content rejection: %v %+v", err, outcomes)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if state == "REVIEW_PENDING" {
				if outcomes.handoffs != 1 || outcomes.outcome != nil {
					t.Fatalf("pending=%+v", outcomes)
				}
			} else if outcomes.handoffs != 0 || outcomes.outcome == nil || outcomes.outcome.State != "BLOCKED_CONTENT" {
				t.Fatalf("unsupported=%+v", outcomes)
			}
		})
	}
}

func TestReviewPreparationPreservesStructuredContentRejection(t *testing.T) {
	t.Parallel()
	rejection := diagnostic.Rejection{
		Code: "MULTI_DISC_TOTAL_BYTES_EXCEEDED", RelativePath: "game/discs.m3u",
		Limit: &diagnostic.Limit{Metric: "TOTAL_BYTES", Actual: 1436977078, Maximum: 1073741824},
	}
	source := &preparationSource{failure: &library.ContentRejectedError{Rejection: rejection}}
	outcomes := &preparationOutcomes{}
	err := NewReviewPreparation(source, outcomes, outcomes).Create(t.Context(), Work{}, ExecutionItem{}, nil)
	if err != nil || outcomes.outcome == nil || outcomes.outcome.Retryable || outcomes.handoffs != 0 {
		t.Fatalf("content decision became retryable work: %v %+v", err, outcomes)
	}
	got := outcomes.outcome
	if got.State != "BLOCKED_CONTENT" || got.Code != rejection.Code || got.Failure == nil ||
		got.Failure.ContentRejection == nil || *got.Failure.ContentRejection.Limit != *rejection.Limit {
		t.Fatalf("content evidence lost: %+v", got)
	}
}

func TestReviewPreparationClassifiesOnlyGroupingAsBlockedContent(t *testing.T) {
	t.Parallel()
	for _, grouping := range []bool{false, true} {
		name := "input failure"
		if grouping {
			name = "grouping"
		}
		t.Run(name, func(t *testing.T) {
			failure := library.ErrInvalid
			if grouping {
				failure = errors.Join(library.ErrInvalid, library.ErrSourceGrouping)
			}
			source := &preparationSource{failure: failure}
			outcomes := &preparationOutcomes{}
			err := NewReviewPreparation(source, outcomes, outcomes).Create(t.Context(), Work{}, ExecutionItem{}, nil)
			if grouping {
				if err != nil || outcomes.outcome == nil || outcomes.outcome.State != "BLOCKED_CONTENT" {
					t.Fatalf("grouping not closed: %v %+v", err, outcomes)
				}
			} else if !errors.Is(err, library.ErrInvalid) || outcomes.outcome != nil {
				t.Fatalf("input error disguised: %v %+v", err, outcomes)
			}
		})
	}
}

func TestReviewPreparationRejectsAmbiguousBoundResults(t *testing.T) {
	t.Parallel()
	source := &preparationSource{found: true, result: library.ServerImportResult{Created: library.ServerCreated{ImportJobID: "job"}, Items: []library.ServerImportItem{
		{ItemID: "primary", SourceRelativePaths: []string{"child.zip"}}, {ItemID: "companion", SourceRelativePaths: []string{"parent.zip"}},
	}}}
	outcomes := &preparationOutcomes{}
	_, err := NewReviewPreparation(source, outcomes, outcomes).Resume(t.Context(), Work{}, ExecutionItem{Files: []ExecutionFile{{Path: "child.zip"}}})
	if !errors.Is(err, ErrInvalid) || outcomes.resumed || outcomes.handoffs != 0 || outcomes.outcome != nil {
		t.Fatalf("ambiguous review selected: %v %+v", err, outcomes)
	}
}
