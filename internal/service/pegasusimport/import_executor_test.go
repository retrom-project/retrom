package pegasusimport

import (
	"context"
	"errors"
	"reflect"
	"testing"

	libraryimportmodel "retrom/internal/model/libraryimport"
	model "retrom/internal/model/pegasusimport"
)

func TestImportExecutorPreservesFailedOutcomeCauseAndStopsClaiming(t *testing.T) {
	t.Parallel()
	fake, executor := newImportExecutorFixture()
	copyFailure, finishFailure := errors.New("source unavailable"), errors.New("outcome unavailable")
	fake.failures["copy:game.gba"], fake.failures["finish"] = copyFailure, finishFailure
	err := executor.Execute(t.Context(), model.Work{})
	if !errors.Is(err, copyFailure) || !errors.Is(err, finishFailure) || fake.claims != 1 {
		t.Fatalf("lost failure or continued: error=%v claims=%d events=%v", err, fake.claims, fake.events)
	}
	if len(fake.outcomes) != 1 || fake.outcomes[0].State != "READ_FAILED" || !fake.outcomes[0].Retryable {
		t.Fatalf("wrong source outcome: %+v", fake.outcomes)
	}
}

func TestImportExecutorReplaysReviewBeforeAnySourceAccess(t *testing.T) {
	t.Parallel()
	fake, executor := newImportExecutorFixture()
	fake.resumed = true
	if err := executor.Process(t.Context(), model.Work{}, fake.items[0]); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fake.events, []string{"resume"}) {
		t.Fatalf("replay touched source or rewrote outcome: %v", fake.events)
	}
}

func TestImportExecutorCopiesAndBindsBeforeCreatingReview(t *testing.T) {
	t.Parallel()
	fake, executor := newImportExecutorFixture()
	if err := executor.Execute(t.Context(), model.Work{}); err != nil {
		t.Fatal(err)
	}
	expected := []string{
		"cancel", "next", "resume", "phase:COPYING_CONTENT", "copy:game.gba", "bind:game.gba",
		"checkpoint", "phase:VALIDATING", "review", "cancel", "next", "complete",
	}
	if !reflect.DeepEqual(fake.events, expected) || len(fake.outcomes) != 0 {
		t.Fatalf("flow=%v outcomes=%v", fake.events, fake.outcomes)
	}
	if len(fake.reviewFiles) != 1 || fake.reviewFiles[0].BlobID != "blob:game.gba" {
		t.Fatalf("review received unbound files: %+v", fake.reviewFiles)
	}
}

func TestImportExecutorLostOwnershipCannotBecomeItemFailure(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{model.ErrVersionConflict, libraryimportmodel.ErrVersionConflict, context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			fake, executor := newImportExecutorFixture()
			fake.failures["resume"] = cause
			err := executor.Execute(t.Context(), model.Work{})
			if !errors.Is(err, cause) || fake.claims != 1 || len(fake.outcomes) != 0 {
				t.Fatalf("ownership loss attempted settlement: err=%v claims=%d outcomes=%v", err, fake.claims, fake.outcomes)
			}
		})
	}
}

func TestImportExecutorCancelledContextNeverTouchesRepositories(t *testing.T) {
	t.Parallel()
	fake, executor := newImportExecutorFixture()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := executor.Execute(ctx, model.Work{}); !errors.Is(err, context.Canceled) || len(fake.events) != 0 {
		t.Fatalf("cancelled work proceeded: err=%v events=%v", err, fake.events)
	}
}

func TestImportExecutorMediaWarningsPreservePlayableSource(t *testing.T) {
	t.Parallel()
	for _, changed := range []bool{false, true} {
		name := "invalid"
		if changed {
			name = "changed"
		}
		t.Run(name, func(t *testing.T) {
			fake, executor := newImportExecutorFixture()
			fake.items[0].Assets = []model.ExecutionAsset{{Kind: "COVER", Path: "cover.png", Size: 4}}
			if changed {
				fake.failures["media:cover.png"] = model.ErrSourceChanged
			}
			if err := executor.Execute(t.Context(), model.Work{}); err != nil {
				t.Fatal(err)
			}
			code := "PEGASUS_IMAGE_INVALID"
			if changed {
				code = "PEGASUS_SOURCE_CHANGED"
			}
			if len(fake.outcomes) != 0 || fake.warning != code || len(fake.reviewFiles) != 1 {
				t.Fatalf("media error blocked content: outcomes=%v warning=%s review=%v", fake.outcomes, fake.warning, fake.reviewFiles)
			}
		})
	}
}

func TestImportExecutorStorageFailureCannotCreateReview(t *testing.T) {
	t.Parallel()
	for _, step := range []string{"bind:game.gba", "warning:cover.png", "bind:cover.png", "phase:VALIDATING"} {
		t.Run(step, func(t *testing.T) {
			fake, executor := newImportExecutorFixture()
			fake.items[0].Assets = []model.ExecutionAsset{{Kind: "COVER", Path: "cover.png", Size: 4}}
			fake.validMedia = step != "warning:cover.png"
			fake.failures[step] = errors.New("write unavailable")
			if err := executor.Execute(t.Context(), model.Work{}); err != nil {
				t.Fatal(err)
			}
			if len(fake.outcomes) != 1 || fake.outcomes[0].State != "COMMIT_FAILED" || len(fake.reviewFiles) != 0 {
				t.Fatalf("partial material created review: outcomes=%v files=%v", fake.outcomes, fake.reviewFiles)
			}
		})
	}
}

func TestImportExecutorCancellationAfterCopyClosesCurrentItem(t *testing.T) {
	t.Parallel()
	fake, executor := newImportExecutorFixture()
	fake.cancelled = true
	if err := executor.Process(t.Context(), model.Work{}, fake.items[0]); err != nil {
		t.Fatal(err)
	}
	if len(fake.outcomes) != 1 || fake.outcomes[0].State != "CANCELLED" || len(fake.reviewFiles) != 0 {
		t.Fatalf("cancel did not stop review creation: outcomes=%v files=%v", fake.outcomes, fake.reviewFiles)
	}
}

func TestImportExecutorCompanionsUseBoundIdentities(t *testing.T) {
	t.Parallel()
	fake, executor := newImportExecutorFixture()
	item := fake.items[0]
	item.TargetPlatformKind = "arcade"
	item.Files[0].Path = "child.zip"
	fake.companions = []model.CompanionCandidate{
		{ItemID: "parent", File: model.ExecutionFile{Path: "parent.zip", Size: 4}},
		{ItemID: "changed", File: model.ExecutionFile{Path: "changed.zip", Size: 4}},
	}
	fake.failures["copy:changed.zip"] = model.ErrSourceChanged
	if err := executor.Process(t.Context(), model.Work{}, item); err != nil {
		t.Fatal(err)
	}
	if len(fake.reviewFiles) != 2 || fake.reviewFiles[1].BlobID != "companion:parent.zip" || len(fake.outcomes) != 0 {
		t.Fatalf("incorrect Arcade assembly: files=%v outcomes=%v", fake.reviewFiles, fake.outcomes)
	}
}

func TestImportExecutorLibraryFailureKeepsInputLimitDiagnostics(t *testing.T) {
	t.Parallel()
	fake, executor := newImportExecutorFixture()
	fake.failures["review"] = libraryimportmodel.ErrInvalid
	if err := executor.Process(t.Context(), model.Work{}, fake.items[0]); err != nil {
		t.Fatal(err)
	}
	if len(fake.outcomes) != 1 {
		t.Fatalf("outcomes=%+v", fake.outcomes)
	}
	outcome := fake.outcomes[0]
	if outcome.Code != "PEGASUS_LIBRARY_IMPORT_FAILED" || outcome.Failure == nil ||
		outcome.Failure.CauseCode != "LIBRARY_IMPORT_INPUT_INVALID" || outcome.Failure.ObservedFileCount == nil ||
		*outcome.Failure.ObservedFileCount != 1 {
		t.Fatalf("library error lost diagnostics: %+v", outcome)
	}
}
