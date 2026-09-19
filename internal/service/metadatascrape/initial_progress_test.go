package metadatascrape

import (
	"context"
	"errors"
	"testing"

	metadatascrapemodel "retrom/internal/model/metadatascrape"
)

type initialMemory struct {
	metadatascrapemodel.InitialReader
	item       metadatascrapemodel.InitialImport
	found      bool
	readsError error
	changes    []metadatascrapemodel.InitialProgressChange
}

func (memory *initialMemory) Import(context.Context, string) (metadatascrapemodel.InitialImport, bool, error) {
	return memory.item, memory.found, memory.readsError
}

func (memory *initialMemory) Candidates(context.Context, string) ([]metadatascrapemodel.InitialCandidate, error) {
	return nil, nil
}

func (memory *initialMemory) Apply(context.Context, metadatascrapemodel.InitialDraftChange) error {
	return nil
}

func (memory *initialMemory) Advance(_ context.Context, change metadatascrapemodel.InitialProgressChange) error {
	memory.changes = append(memory.changes, change)
	return nil
}

func TestInitialReviewProgressDistinguishesPendingAndPartialFailure(t *testing.T) {
	for _, test := range []struct {
		name                      string
		running, failed, rejected int64
		want                      string
	}{
		{"last clean", 1, 0, 0, "REVIEW_PENDING"},
		{"last with failed item", 1, 1, 0, "PARTIAL_FAILURE"},
		{"last with rejected file", 1, 0, 1, "PARTIAL_FAILURE"},
		{"other active items", 2, 0, 0, "RUNNING"},
	} {
		t.Run(test.name, func(t *testing.T) {
			memory := &initialMemory{found: true, item: metadatascrapemodel.InitialImport{ItemID: "item", ImportJobID: "import", ItemState: "SCRAPING", Running: test.running, Failed: test.failed, Rejected: test.rejected, Version: 7}}
			if err := NewInitialReview(metadatascrapemodel.InitialReviewScope{Read: memory, Write: memory}).Complete(t.Context(), "run", 100); err != nil {
				t.Fatal(err)
			}
			if len(memory.changes) != 1 {
				t.Fatalf("progress writes=%d", len(memory.changes))
			}
			change := memory.changes[0]
			if change.JobState != test.want || change.ItemState != "REVIEW_PENDING" || change.ReviewDelta != 1 || change.FailedDelta != 0 || change.ExpectedVersion != 7 || change.Now != 100 {
				t.Fatalf("initial progress: %+v", change)
			}
		})
	}
}

func TestInitialReviewRejectsInvalidProgressBeforeWriting(t *testing.T) {
	memory := &initialMemory{found: true, item: metadatascrapemodel.InitialImport{ItemState: "SCRAPING", Running: 0}}
	err := NewInitialReview(metadatascrapemodel.InitialReviewScope{Read: memory, Write: memory}).Complete(t.Context(), "run", 100)
	if !errors.Is(err, metadatascrapemodel.ErrInitialProgressState) || len(memory.changes) != 0 {
		t.Fatalf("invalid progress wrote: %v", err)
	}
	memory.readsError = context.Canceled
	err = NewInitialReview(metadatascrapemodel.InitialReviewScope{Read: memory, Write: memory}).Complete(t.Context(), "run", 100)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost read failure: %v", err)
	}
}

func TestInitialReviewFailureRetainsScrapingStageAndCode(t *testing.T) {
	memory := &initialMemory{found: true, item: metadatascrapemodel.InitialImport{ItemState: "SCRAPING", Running: 1}}
	err := NewInitialReview(metadatascrapemodel.InitialReviewScope{Read: memory, Write: memory}).Fail(t.Context(), "run", "STORAGE_FAILED", 100)
	if err != nil {
		t.Fatal(err)
	}
	change := memory.changes[0]
	if change.ItemState != "FAILED_RETRYABLE" || change.JobState != "PARTIAL_FAILURE" || change.FailedDelta != 1 || change.ReviewDelta != 0 || *change.FailedStage != "SCRAPING" || *change.ErrorCode != "STORAGE_FAILED" {
		t.Fatalf("failure progress: %+v", change)
	}
}

func TestInitialAssetsChooseReadyImagesByOrdinalWithoutMutatingInput(t *testing.T) {
	assets := []metadatascrapemodel.InitialAsset{{ID: "later", Kind: "COVER", Ordinal: 2}, {ID: "shot", Kind: "SCREENSHOT", Ordinal: 0}, {ID: "first", Kind: "COVER", Ordinal: 1}}
	var change metadatascrapemodel.InitialDraftChange
	selectInitialAssets(&change, assets)
	if change.CoverID == nil || *change.CoverID != "first" || change.BackgroundID != nil || len(change.Screenshots) != 1 || change.Screenshots[0].ID != "shot" {
		t.Fatalf("asset selection: %+v", change)
	}
	if assets[0].ID != "later" {
		t.Fatal("selection changed caller asset order")
	}
}
