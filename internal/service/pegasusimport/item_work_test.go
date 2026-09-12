package pegasusimport

import (
	"context"
	"errors"
	"testing"
	"time"
)

type itemWorkFake struct {
	execution ExecutionSnapshot
	item      ExecutionItem
	saved     bool
	failure   error
}

func (fake *itemWorkFake) WithItemWork(_ context.Context, work func(ItemWorkScope) error) error {
	return work(ItemWorkScope{Read: fake, Write: fake})
}

func (fake *itemWorkFake) Execution(context.Context, string) (ExecutionSnapshot, error) {
	return fake.execution, fake.failure
}

func (fake *itemWorkFake) Next(context.Context, string) (ExecutionItem, bool, error) {
	return fake.item, true, fake.failure
}

func (fake *itemWorkFake) Current(context.Context, string) (OwnedItem, error) {
	return OwnedItem{Execution: fake.execution, Item: fake.item}, fake.failure
}

func (fake *itemWorkFake) Claim(context.Context, ItemClaim) error {
	fake.saved = true
	return fake.failure
}

func (fake *itemWorkFake) Resume(context.Context, ItemResume) error {
	fake.saved = true
	return fake.failure
}

func (fake *itemWorkFake) Finish(context.Context, ItemFinish) error {
	fake.saved = true
	return fake.failure
}

func itemWorkFixture() (*itemWorkFake, ExecutionIdentity) {
	completion, identity := completionFixture()
	return &itemWorkFake{execution: completion.before, item: ExecutionItem{ID: "item", ImportID: "import", State: "PENDING", Version: 1}}, identity
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
				err = service.Finish(t.Context(), identity, "item", ItemOutcome{State: "COMMIT_FAILED", Code: "INTERNAL_ERROR"})
			}
			if !errors.Is(err, ErrVersionConflict) || fake.saved {
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
	if _, found, err := service.Next(t.Context(), identity); !errors.Is(err, ErrVersionConflict) || found || fake.saved {
		t.Fatalf("canceled claim=%v %v", found, err)
	}
	fake.item.State = "COPYING"
	if err := service.Finish(t.Context(), identity, "item", ItemOutcome{State: "CANCELLED", Code: "CANCELLED"}); err != nil || !fake.saved {
		t.Fatalf("current cancel finish=%v", err)
	}
}

func TestItemWorkBoundResumeChecksPermanentIdentity(t *testing.T) {
	t.Parallel()
	fake, identity := itemWorkFixture()
	fake.item.State, fake.item.LibraryImportJobID, fake.item.LibraryImportItemID = "COPYING", "library-job", "library-item"
	service := NewItemWork(fake, func() time.Time { return time.UnixMilli(10) })
	if err := service.Resume(t.Context(), identity, "item", "foreign", "library-item"); !errors.Is(err, ErrVersionConflict) || fake.saved {
		t.Fatalf("foreign binding resumed: %v", err)
	}
	if err := service.Resume(t.Context(), identity, "item", "library-job", "library-item"); err != nil || !fake.saved {
		t.Fatalf("bound resume=%v", err)
	}
}
