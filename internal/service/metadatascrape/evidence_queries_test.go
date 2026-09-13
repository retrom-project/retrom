package metadatascrape

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type reviewEvidenceFixture struct {
	candidates             []ReviewCandidateRecord
	assets                 []CandidateAssetView
	runs                   []ReviewRun
	assetsError, runsError error
	assetRequests          [][]string
}

func (fixture *reviewEvidenceFixture) ReviewCandidates(context.Context, string) ([]ReviewCandidateRecord, error) {
	return fixture.candidates, nil
}

func (fixture *reviewEvidenceFixture) CandidateAssets(_ context.Context, ids []string) ([]CandidateAssetView, error) {
	fixture.assetRequests = append(fixture.assetRequests, append([]string(nil), ids...))
	return fixture.assets, fixture.assetsError
}

func (fixture *reviewEvidenceFixture) ReviewRuns(context.Context, string) ([]ReviewRun, error) {
	return fixture.runs, fixture.runsError
}

func TestReviewEvidenceKeepsCandidateOrderingAndAssetOwnership(t *testing.T) {
	t.Parallel()
	fixture := &reviewEvidenceFixture{
		candidates: []ReviewCandidateRecord{{ID: "later-run", MetadataJSON: `{"title":"one"}`, EvidenceJSON: `{}`}, {ID: "earlier-run", MetadataJSON: `{}`, EvidenceJSON: `{}`}},
		assets:     []CandidateAssetView{{CandidateID: "earlier-run", ID: "other-cover"}, {CandidateID: "later-run", ID: "failed-cover", Status: "FAILED"}},
	}
	result, err := NewEvidenceQueries(fixture).Review(t.Context(), "item")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 2 || result.Candidates[0].ID != "later-run" ||
		len(result.Candidates[0].Assets) != 1 || result.Candidates[0].Assets[0].ID != "failed-cover" ||
		len(result.Candidates[1].Assets) != 1 || result.Candidates[1].Assets[0].ID != "other-cover" || result.Runs == nil {
		t.Fatalf("candidate projection = %+v", result)
	}
	if !reflect.DeepEqual(fixture.assetRequests, [][]string{{"later-run", "earlier-run"}}) {
		t.Fatalf("asset requests=%v", fixture.assetRequests)
	}
}

func TestReviewEvidenceReturnsEmptyArraysAndSkipsEmptyAssetLookup(t *testing.T) {
	t.Parallel()
	fixture := &reviewEvidenceFixture{}
	result, err := NewEvidenceQueries(fixture).Review(t.Context(), "item")
	if err != nil || result.Candidates == nil || result.Runs == nil || len(fixture.assetRequests) != 0 {
		t.Fatalf("empty evidence=%+v err=%v requests=%v", result, err, fixture.assetRequests)
	}
}

func TestReviewEvidencePreservesCauseAndClearsPartialCandidates(t *testing.T) {
	t.Parallel()
	cause := errors.New("scrape runs unavailable")
	fixture := &reviewEvidenceFixture{candidates: []ReviewCandidateRecord{{ID: "candidate", MetadataJSON: `{}`, EvidenceJSON: `{}`}}, runsError: cause}
	result, err := NewEvidenceQueries(fixture).Review(t.Context(), "item")
	if !errors.Is(err, cause) || !reflect.DeepEqual(result, ReviewEvidence{}) {
		t.Fatalf("partial evidence=%+v err=%v", result, err)
	}
	fixture.runsError = nil
	fixture.candidates[0].MetadataJSON = `{"broken"`
	result, err = NewEvidenceQueries(fixture).Review(t.Context(), "item")
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) || !reflect.DeepEqual(result, ReviewEvidence{}) {
		t.Fatalf("malformed evidence=%+v err=%v", result, err)
	}
}
