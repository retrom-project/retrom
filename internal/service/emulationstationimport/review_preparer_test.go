package emulationstationimport

import (
	"context"
	"errors"
	"reflect"
	"testing"

	model "retrom/internal/model/emulationstationimport"
	libraryimportmodel "retrom/internal/model/libraryimport"
)

type reviewPreparerMemory struct {
	result                                                              libraryimportmodel.ServerImportResult
	request                                                             libraryimportmodel.OwnedServerSourceRequest
	intent                                                              libraryimportmodel.SourceCreationIntent
	handoff                                                             model.ReviewHandoffRequest
	outcome                                                             model.ItemOutcome
	found                                                               bool
	lookupErr, createErr, finishErr, handoffErr, companionErr, phaseErr error
	calls                                                               []string
}

func (memory *reviewPreparerMemory) LookupOwnedServerSource(
	_ context.Context,
	intent libraryimportmodel.SourceCreationIntent,
) (libraryimportmodel.ServerImportResult, bool, error) {
	memory.calls = append(memory.calls, "lookup")
	memory.intent = intent
	return memory.result, memory.found, memory.lookupErr
}

func (memory *reviewPreparerMemory) CreateOwnedServerSource(
	_ context.Context,
	request libraryimportmodel.OwnedServerSourceRequest,
) (libraryimportmodel.ServerImportResult, error) {
	memory.calls = append(memory.calls, "create")
	memory.request = request
	return memory.result, memory.createErr
}

func (memory *reviewPreparerMemory) Files(
	context.Context, model.Execution, model.ExecutionItem,
) ([]libraryimportmodel.ServerSourceFile, error) {
	memory.calls = append(memory.calls, "companions")
	return []libraryimportmodel.ServerSourceFile{{RelativePath: "parent.zip", BlobID: "parent", SizeBytes: 2}}, memory.companionErr
}

func (memory *reviewPreparerMemory) Resume(context.Context, model.Execution, string, string, string) error {
	memory.calls = append(memory.calls, "resume")
	return nil
}

func (memory *reviewPreparerMemory) Finish(_ context.Context, _ model.Execution, _ string, outcome model.ItemOutcome) error {
	memory.calls = append(memory.calls, "finish")
	memory.outcome = outcome
	return memory.finishErr
}

func (memory *reviewPreparerMemory) Complete(_ context.Context, request model.ReviewHandoffRequest) error {
	memory.calls = append(memory.calls, "handoff")
	memory.handoff = request
	return memory.handoffErr
}

func (memory *reviewPreparerMemory) SetPhase(_ context.Context, _ model.Execution, phase string) error {
	memory.calls = append(memory.calls, phase)
	return memory.phaseErr
}
func (*reviewPreparerMemory) Sanitize(error) string      { return "safe diagnostic" }
func (*reviewPreparerMemory) DatabaseCause(error) string { return "DATABASE_BUSY" }
func newReviewPreparerMemory() *reviewPreparerMemory {
	return &reviewPreparerMemory{
		result: libraryimportmodel.ServerImportResult{Created: libraryimportmodel.ServerCreated{ImportJobID: "ordinary-job"}, Items: []libraryimportmodel.ServerImportItem{{ItemID: "ordinary-item", State: "REVIEW_PENDING"}}},
	}
}

func (memory *reviewPreparerMemory) service() *ReviewPreparer {
	return NewReviewPreparer(
		ReviewPreparerDependencies{Sources: memory, Companions: memory, Items: memory, Phases: memory, Handoff: memory, Diagnostics: memory},
	)
}

func reviewPreparerInputs() (model.Execution, model.ExecutionItem) {
	return model.Execution{
			JobID:           "job",
			ImportID:        "import",
			WorkerID:        "worker",
			ExecutionNo:     3,
			Attempt:         2,
			CreatedByUserID: "actor",
			ReleaseYearMax:  2027,
		}, model.ExecutionItem{
			ID:               "source",
			MetadataJSON:     `{"title":"Frozen"}`,
			TargetPlatformID: "catalog",
			ContentKind:      "STANDARD",
			TagIDs:           []string{"tag"},
			Files:            []model.ExecutionFile{{Path: "game.nes", BlobID: "primary", Size: 5}},
		}
}

func TestReviewPreparerLooksUpPermanentBindingBeforeSources(t *testing.T) {
	memory := newReviewPreparerMemory()
	memory.found = true
	unit, item := reviewPreparerInputs()
	resumed, err := memory.service().Resume(t.Context(), unit, item)
	if err != nil || !resumed || !reflect.DeepEqual(
		memory.calls,
		[]string{"lookup", "resume", "PREPARING_REVIEWS", "handoff"},
	) {
		t.Fatalf("resumed=%v error=%v calls=%v", resumed, err, memory.calls)
	}
	if memory.intent.Kind != libraryimportmodel.SourceOwnerEmulationStation || memory.intent.WorkerID != unit.WorkerID || memory.intent.ExecutionNo != 3 || memory.intent.Attempt != 2 || memory.handoff.Execution != unit {
		t.Fatalf("intent=%#v handoff=%#v", memory.intent, memory.handoff)
	}
}

func TestReviewPreparerFreezesContentModeAndOrdinaryIdentity(t *testing.T) {
	for _, kind := range []string{"STANDARD", "MULTI_DISC"} {
		t.Run(kind, func(t *testing.T) {
			memory := newReviewPreparerMemory()
			unit, item := reviewPreparerInputs()
			item.ContentKind = kind
			if err := memory.service().Create(t.Context(), unit, item); err != nil {
				t.Fatal(err)
			}
			if memory.request.ContentMode != kind || memory.request.AssignedByUserID != unit.CreatedByUserID || memory.request.Intent.Kind != libraryimportmodel.SourceOwnerEmulationStation || !reflect.DeepEqual(
				memory.request.TagIDs,
				item.TagIDs,
			) || len(
				memory.request.Files,
			) != 2 {
				t.Fatalf("request=%#v", memory.request)
			}
			if memory.handoff.LibraryJobID != "ordinary-job" || memory.handoff.LibraryItemID != "ordinary-item" {
				t.Fatalf("handoff=%#v", memory.handoff)
			}
		})
	}
}

func TestReviewPreparerReturnsOwnershipAndLookupCauses(t *testing.T) {
	cause := errors.New("query failed")
	for _, stage := range []string{"lookup", "create-owner", "handoff-owner", "phase"} {
		t.Run(stage, func(t *testing.T) {
			memory := newReviewPreparerMemory()
			unit, item := reviewPreparerInputs()
			expected := cause
			switch stage {
			case "lookup":
				memory.lookupErr = cause
			case "create-owner":
				memory.createErr = libraryimportmodel.ErrVersionConflict
				expected = libraryimportmodel.ErrVersionConflict
			case "handoff-owner":
				memory.handoffErr = model.ErrVersionConflict
				expected = model.ErrVersionConflict
			case "phase":
				memory.phaseErr = cause
			}
			var err error
			if stage == "lookup" {
				_, err = memory.service().Resume(t.Context(), unit, item)
			} else {
				err = memory.service().Create(t.Context(), unit, item)
			}
			if !errors.Is(err, expected) || memory.outcome.State != "" {
				t.Fatalf("error=%v outcome=%#v", err, memory.outcome)
			}
		})
	}
}

func TestReviewPreparerRecordsFailureButRetainsLateWriteCause(t *testing.T) {
	cause, write := errors.New("handoff database failed"), errors.New("outcome database failed")
	memory := newReviewPreparerMemory()
	unit, item := reviewPreparerInputs()
	memory.handoffErr = cause
	memory.finishErr = write
	err := memory.service().Create(t.Context(), unit, item)
	if !errors.Is(
		err,
		cause,
	) || !errors.Is(
		err,
		write,
	) || memory.outcome.State != "COMMIT_FAILED" || !memory.outcome.Retryable {
		t.Fatalf("error=%v outcome=%#v", err, memory.outcome)
	}
	failure := memory.outcome.Failure
	if failure == nil || failure.LibraryImportJobID == nil || *failure.LibraryImportJobID != "ordinary-job" || failure.LibraryImportItemID == nil || *failure.LibraryImportItemID != "ordinary-item" || failure.CauseCode != "DATABASE_BUSY" {
		t.Fatalf("failure=%#v", failure)
	}
}
