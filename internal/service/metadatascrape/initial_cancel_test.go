package metadatascrape

import (
	"context"
	"testing"

	metadatascrapemodel "retrom/internal/model/metadatascrape"
)

type cancelledInitialMemory struct{ initialMemory }

func (memory *cancelledInitialMemory) Candidates(context.Context, string) ([]metadatascrapemodel.InitialCandidate, error) {
	panic("cancelled run must not apply candidates")
}

func TestInitialCancellationDistinguishesParentFromIndividualRequest(t *testing.T) {
	for _, test := range []struct {
		parent             bool
		running            int64
		item, batch        string
		reviews, cancelled int64
	}{
		{false, 1, "REVIEW_PENDING", "REVIEW_PENDING", 1, 0},
		{true, 1, "CANCELLED", "CANCELLED", 0, 1},
		{true, 2, "CANCELLED", "CANCEL_REQUESTED", 0, 1},
	} {
		memory := &cancelledInitialMemory{initialMemory{found: true, item: metadatascrapemodel.InitialImport{ItemState: "SCRAPING", Running: test.running}}}
		if err := NewInitialReview(metadatascrapemodel.InitialReviewScope{Read: memory, Write: memory}).Cancel(t.Context(), "run", test.parent, 100); err != nil {
			t.Fatal(err)
		}
		change := memory.changes[0]
		if change.ItemState != test.item || change.JobState != test.batch || change.ReviewDelta != test.reviews || change.CancelledDelta != test.cancelled {
			t.Fatalf("cancel projection: %+v", change)
		}
	}
}
