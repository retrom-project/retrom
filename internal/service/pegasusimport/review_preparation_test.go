package pegasusimport

import (
	"context"
	"errors"
	model "retrom/internal/model/pegasusimport"
	"testing"

	library "retrom/internal/model/libraryimport"
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
	outcome  *model.ItemOutcome
	handoffs int
	failure  error
}

func (fake *preparationOutcomes) Resume(context.Context, model.ExecutionIdentity, string, string, string) error {
	fake.resumed = true
	return fake.failure
}

func (fake *preparationOutcomes) Finish(_ context.Context, _ model.ExecutionIdentity, _ string, outcome model.ItemOutcome) error {
	fake.outcome = &outcome
	return fake.failure
}

func (fake *preparationOutcomes) Complete(context.Context, model.ReviewHandoffRequest) error {
	fake.handoffs++
	return fake.failure
}

func TestReviewPreparationReplaysPermanentBindingWithoutSourcePaths(t *testing.T) {
	t.Parallel()
	source := &preparationSource{found: true, result: library.ServerImportResult{Created: library.ServerCreated{ImportJobID: "library-job"}, Items: []library.ServerImportItem{{ItemID: "library-item", State: "DISCARDED", ExistingGameID: "game-a", ExistingMatches: []library.ServerDuplicateMatch{{GameID: "game-a"}, {GameID: "game-b"}}}}}}
	outcomes := &preparationOutcomes{}
	service := NewReviewPreparation(source, outcomes, outcomes)
	found, err := service.Resume(t.Context(), model.Work{ImportID: "import", JobID: "job", WorkerID: "owner", ExecutionNo: 1, Attempt: 1}, model.ExecutionItem{ID: "item"})
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
	outcomes := &preparationOutcomes{failure: model.ErrVersionConflict}
	_, err := NewReviewPreparation(source, outcomes, outcomes).Resume(t.Context(), model.Work{}, model.ExecutionItem{})
	if !errors.Is(err, model.ErrVersionConflict) || outcomes.handoffs != 0 || outcomes.outcome != nil {
		t.Fatalf("stale replay wrote review: %v %+v", err, outcomes)
	}
}

func TestReviewPreparationSeedsOnlyOnePendingReview(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"REVIEW_PENDING", "BLOCKED"} {
		t.Run(state, func(t *testing.T) {
			source := &preparationSource{result: library.ServerImportResult{Created: library.ServerCreated{ImportJobID: "job"}, Items: []library.ServerImportItem{{ItemID: "item", State: state}}}}
			outcomes := &preparationOutcomes{}
			err := NewReviewPreparation(source, outcomes, outcomes).Create(t.Context(), model.Work{}, model.ExecutionItem{Files: []model.ExecutionFile{{Path: "game.zip"}}}, []library.ServerSourceFile{{RelativePath: "game.zip"}})
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
			err := NewReviewPreparation(source, outcomes, outcomes).Create(t.Context(), model.Work{}, model.ExecutionItem{}, nil)
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
	_, err := NewReviewPreparation(source, outcomes, outcomes).Resume(t.Context(), model.Work{}, model.ExecutionItem{Files: []model.ExecutionFile{{Path: "child.zip"}}})
	if !errors.Is(err, model.ErrInvalid) || outcomes.resumed || outcomes.handoffs != 0 || outcomes.outcome != nil {
		t.Fatalf("ambiguous review selected: %v %+v", err, outcomes)
	}
}
