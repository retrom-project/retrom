package metadatascrape

import (
	"encoding/json"
	"testing"

	metadatascrapemodel "retrom/internal/model/metadatascrape"
)

func TestInitialReviewMergePreservesMissingCandidateFields(t *testing.T) {
	value, title, err := mergeInitialReviewMetadata(`{"title":"old","description":"kept","players":2}`, `{"title":"new","description":"  ","players":0}`)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Title, Description string
		Players            int
	}
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		t.Fatal(err)
	}
	if title != "new" || decoded.Title != "new" || decoded.Description != "kept" || decoded.Players != 0 {
		t.Fatalf("merged metadata: %s", value)
	}
}

func TestInitialCandidateUsesHitsThenEvidenceOrderThenProviderID(t *testing.T) {
	candidates := []metadatascrapemodel.InitialCandidate{
		{ID: "low-hits", HitCount: 1, FirstQueryOrder: 0, ProviderGameID: "a"},
		{ID: "later-query", HitCount: 2, FirstQueryOrder: 2, ProviderGameID: "a"},
		{ID: "later-provider", HitCount: 2, FirstQueryOrder: 1, ProviderGameID: "b"},
		{ID: "chosen", HitCount: 2, FirstQueryOrder: 1, ProviderGameID: "a"},
	}
	chosen, found := selectInitialCandidate(candidates)
	if !found || chosen.ID != "chosen" {
		t.Fatalf("selected candidate: %+v", chosen)
	}
	if candidates[0].ID != "low-hits" {
		t.Fatal("selection mutated caller evidence")
	}
	if _, found := selectInitialCandidate(nil); found {
		t.Fatal("empty evidence manufactured candidate")
	}
}
