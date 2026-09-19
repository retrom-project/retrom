package pegasusimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/pegasusimport"
)

type itemWorkFake struct {
	execution model.ExecutionSnapshot
	item      model.ExecutionItem
	outcome   model.ItemOutcome
	saved     bool
	failure   error
}

func (fake *itemWorkFake) ClaimNextItem(_ context.Context, unit model.ExecutionIdentity, nowMS int64) (model.ClaimNextItemResult, error) {
	if fake.failure != nil {
		return model.ClaimNextItemResult{}, fake.failure
	}
	if err := model.ValidateExecution(fake.execution, unit, nowMS); err != nil {
		return model.ClaimNextItemResult{}, err
	}
	if fake.execution.Kind != "SERVER_PEGASUS_IMPORT" || fake.execution.JobState != "RUNNING" {
		return model.ClaimNextItemResult{}, model.ErrVersionConflict
	}
	item := fake.item
	if item.ImportID != unit.ImportID || item.State != "PENDING" || !model.ValidItemVersion(item.Version) {
		return model.ClaimNextItemResult{}, model.ErrVersionConflict
	}
	fake.saved = true
	item.State, item.Version = "COPYING", item.Version+1
	return model.ClaimNextItemResult{Item: item, Found: true}, nil
}

func (fake *itemWorkFake) CommitItemResume(_ context.Context, unit model.ExecutionIdentity, itemID, jobID, ordinaryID string, nowMS int64) error {
	if fake.failure != nil {
		return fake.failure
	}
	if err := model.ValidateExecution(fake.execution, unit, nowMS); err != nil {
		return err
	}
	if fake.item.ID != itemID || fake.item.ImportID != unit.ImportID || !model.ValidItemVersion(fake.item.Version) {
		return model.ErrVersionConflict
	}
	if jobID == "" || ordinaryID == "" || fake.item.LibraryImportJobID != jobID || fake.item.LibraryImportItemID != ordinaryID {
		return model.ErrVersionConflict
	}
	if fake.item.State == "VALIDATING" || fake.item.State == "REVIEW_PENDING" {
		return nil
	}
	if fake.item.State != "COPYING" {
		return model.ErrVersionConflict
	}
	fake.saved = true
	return nil
}

func (fake *itemWorkFake) CommitItemFinish(_ context.Context, unit model.ExecutionIdentity, itemID string, outcome model.ItemOutcome, nowMS int64) error {
	if fake.failure != nil {
		return fake.failure
	}
	if err := model.ValidateExecution(fake.execution, unit, nowMS); err != nil {
		return err
	}
	if fake.item.ID != itemID || fake.item.ImportID != unit.ImportID || !model.ValidItemVersion(fake.item.Version) {
		return model.ErrVersionConflict
	}
	if fake.item.State == outcome.State {
		return nil
	}
	if fake.item.State != "COPYING" && fake.item.State != "VALIDATING" {
		return model.ErrVersionConflict
	}
	fake.outcome = outcome
	fake.saved = true
	return nil
}

func (fake *itemWorkFake) Execution(context.Context, string) (model.ExecutionSnapshot, error) {
	return fake.execution, fake.failure
}

func (fake *itemWorkFake) Next(context.Context, string) (model.ExecutionItem, bool, error) {
	return fake.item, true, fake.failure
}

func (fake *itemWorkFake) Current(context.Context, string) (model.OwnedItem, error) {
	return model.OwnedItem{Execution: fake.execution, Item: fake.item}, fake.failure
}

func (fake *itemWorkFake) Claim(context.Context, model.ItemClaim) error {
	fake.saved = true
	return fake.failure
}

func (fake *itemWorkFake) Resume(context.Context, model.ItemResume) error {
	fake.saved = true
	return fake.failure
}

func (fake *itemWorkFake) Finish(_ context.Context, change model.ItemFinish) error {
	fake.outcome = change.Outcome
	fake.saved = true
	return fake.failure
}

func itemWorkFixture() (*itemWorkFake, model.ExecutionIdentity) {
	completion, identity := completionFixture()
	return &itemWorkFake{execution: completion.before, item: model.ExecutionItem{ID: "item", ImportID: "import", State: "PENDING", Version: 1}}, identity
}

func TestItemWorkNeverWritesForReplacedOwner(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"next", "resume", "finish"} {
		t.Run(method, func(t *testing.T) {
			fake, identity := itemWorkFixture()
			identity.WorkerID = "previous-worker"
			service := NewItemWork(fake, func() time.Time { return time.UnixMilli(10) })
			var err error
			switch method {
			case "next":
				_, _, err = service.Next(t.Context(), identity)
			case "resume":
				err = service.Resume(t.Context(), identity, "item", "library-job", "library-item")
			case "finish":
				err = service.Finish(t.Context(), identity, "item", model.ItemOutcome{State: "COMMIT_FAILED", Code: "INTERNAL_ERROR"})
			}
			if !errors.Is(err, model.ErrVersionConflict) || fake.saved {
				t.Fatalf("old owner wrote %s: %v", method, err)
			}
		})
	}
}

func TestItemWorkRejectsCancelledClaimButCanFinishCurrentItem(t *testing.T) {
	t.Parallel()
	fake, identity := itemWorkFixture()
	fake.execution.JobState, fake.execution.ImportState = "CANCEL_REQUESTED", "CANCEL_REQUESTED"
	service := NewItemWork(fake, func() time.Time { return time.UnixMilli(10) })
	if _, found, err := service.Next(t.Context(), identity); !errors.Is(err, model.ErrVersionConflict) || found || fake.saved {
		t.Fatalf("canceled claim=%v %v", found, err)
	}
	fake.item.State = "COPYING"
	if err := service.Finish(t.Context(), identity, "item", model.ItemOutcome{State: "CANCELLED", Code: "CANCELLED"}); err != nil || !fake.saved {
		t.Fatalf("current cancel finish=%v", err)
	}
}

func TestItemWorkBoundResumeChecksPermanentIdentity(t *testing.T) {
	t.Parallel()
	fake, identity := itemWorkFixture()
	fake.item.State, fake.item.LibraryImportJobID, fake.item.LibraryImportItemID = "COPYING", "library-job", "library-item"
	service := NewItemWork(fake, func() time.Time { return time.UnixMilli(10) })
	if err := service.Resume(t.Context(), identity, "item", "foreign", "library-item"); !errors.Is(err, model.ErrVersionConflict) || fake.saved {
		t.Fatalf("foreign binding resumed: %v", err)
	}
	if err := service.Resume(t.Context(), identity, "item", "library-job", "library-item"); err != nil || !fake.saved {
		t.Fatalf("bound resume=%v", err)
	}
}
